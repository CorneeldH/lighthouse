import cases from '../../../../tests/pack/fixtures/issue-state/cases.json'
import { describe, expect, it } from 'vitest'
import type { Issue, Repo, Run } from '../../api/lighthouse'
import {
  buildGraph,
  issueState,
  isShaping,
  isUphill,
  milestoneRows,
  placeInRepo,
  priorityOrder,
  sortMilestoneRows,
  withoutMilestone,
} from './derive'

const R = 'a/x'
const iss = (
  number: number,
  by: Array<[string, number]> = [],
  extra: Partial<Issue> = {},
): Issue => ({
  number,
  title: `t${number}`,
  url: `u${number}`,
  status: 'To do',
  label: '',
  open_question: false,
  needs_label: false,
  prio_weight: 0,
  factor: 0,
  blocked_by: by.length ? by.map(([repo, issue]) => ({ repo, issue })) : null,
  ...extra,
})
const repo = (slug: string, issues: Issue[], milestones: Repo['milestones'] = []): Repo => ({
  slug,
  issues,
  milestones,
})
const n = (g: ReturnType<typeof buildGraph>, key: string) => {
  const node = g.nodes.get(key)
  if (!node) throw new Error(`missing ${key}`)
  return node
}

describe('buildGraph', () => {
  it('chain A->B->C has depth 0,1,2 and waiting 2,1,0', () => {
    const g = buildGraph([repo(R, [iss(1), iss(2, [[R, 1]]), iss(3, [[R, 2]])])], [])
    expect([1, 2, 3].map((i) => n(g, `${R}#${i}`).depth)).toEqual([0, 1, 2])
    expect([1, 2, 3].map((i) => n(g, `${R}#${i}`).waiting)).toEqual([2, 1, 0])
  })

  it('diamond counts the shared leaf once', () => {
    const g = buildGraph(
      [repo(R, [iss(1), iss(2, [[R, 1]]), iss(3, [[R, 1]]), iss(4, [[R, 2], [R, 3]])])],
      [],
    )
    expect(n(g, `${R}#1`).waiting).toBe(3)
    expect(n(g, `${R}#4`).depth).toBe(2)
  })

  it('cycle terminates; members share component and depth, do not count each other', () => {
    const g = buildGraph(
      [repo(R, [iss(1, [[R, 2]]), iss(2, [[R, 1]]), iss(3, [[R, 2]])])],
      [],
    )
    const a = n(g, `${R}#1`)
    const b = n(g, `${R}#2`)
    expect(a.cycle && b.cycle).toBe(true)
    expect(n(g, `${R}#3`).cycle).toBe(false)
    expect(a.component).toBe(b.component)
    expect(a.depth).toBe(b.depth)
    expect(a.waiting).toBe(1)
    expect(b.waiting).toBe(1)
    expect(n(g, `${R}#3`).depth).toBe(a.depth + 1)
  })

  it('counts every member of a downstream cycle component', () => {
    const g = buildGraph(
      [repo(R, [iss(1), iss(2, [[R, 1], [R, 3]]), iss(3, [[R, 2]])])],
      [],
    )
    expect(n(g, `${R}#1`).waiting).toBe(2)
  })

  it('self-loop is a cycle, terminates, and counts nobody extra', () => {
    const g = buildGraph([repo(R, [iss(1, [[R, 1]]), iss(2, [[R, 1]])])], [])
    expect(n(g, `${R}#1`)).toMatchObject({ cycle: true, depth: 0, waiting: 1 })
    expect(n(g, `${R}#2`)).toMatchObject({ cycle: false, depth: 1, waiting: 0 })
  })

  it('cross-repo cycle behaves the same', () => {
    const g = buildGraph(
      [repo('a/x', [iss(1, [['b/y', 1]])]), repo('b/y', [iss(1, [['a/x', 1]])])],
      [],
    )
    expect(n(g, 'a/x#1').cycle).toBe(true)
    expect(n(g, 'a/x#1').component).toBe(n(g, 'b/y#1').component)
    expect(n(g, 'a/x#1').waiting).toBe(0)
  })

  it('survives a long chain without recursion', () => {
    const issues = [iss(1)]
    for (let i = 2; i <= 3000; i++) issues.push(iss(i, [[R, i - 1]]))
    const g = buildGraph([repo(R, issues)], [])
    expect(n(g, `${R}#3000`).depth).toBe(2999)
  })

  it('external blocker is a titleless external node', () => {
    const g = buildGraph([repo(R, [iss(1, [['z/q', 9]])])], [])
    const ext = n(g, 'z/q#9')
    expect(ext.external).toBe(true)
    expect(ext.title).toBe('')
    expect(n(g, `${R}#1`).depth).toBeGreaterThanOrEqual(1)
    expect(g.edges).toEqual([{ from: 'z/q#9', to: `${R}#1` }])
  })

  it('an issue without blocked_by has depth 0', () => {
    const g = buildGraph([repo(R, [iss(1)])], [])
    expect(n(g, `${R}#1`).depth).toBe(0)
    expect(g.edges).toEqual([])
  })

  it('marks active and uphill from runs', () => {
    const runs: Run[] = [{ repo: R, issue: 1, state: 'open', phase: 'spec-review' }]
    const g = buildGraph([repo(R, [iss(1), iss(2)])], runs)
    expect(n(g, `${R}#1`)).toMatchObject({ active: true, uphill: true })
    expect(n(g, `${R}#2`)).toMatchObject({ active: false, uphill: false })
  })
})

interface StateCase {
  name: string
  closed?: boolean
  labels?: string[]
  status?: string
  run?: string
  blockers?: Array<{ issue: number; state: string }>
  open_question?: boolean
  want: { state: string; needs_human?: boolean }
}

// Rank of the label keys, lowest first, as the API reports the one winning key.
const LABEL_RANK = [
  'needs_shaping',
  'needs_decision',
  'blocked_issue',
  'blocked_external',
  'needs_hands',
  'needs_testing',
]

// Any needs_* label on the issue, built from the fixture labels like the server does.
const NEEDS_LABELS = ['needs_shaping', 'needs_decision', 'needs_hands', 'needs_testing']

describe('issueState follows the shared state fixture', () => {
  const openCases = (cases as StateCase[]).filter((c) => !c.closed)
  it.each(openCases)('$name', (c) => {
    const label = LABEL_RANK.find((k) => c.labels?.includes(k)) ?? ''
    const open = (c.blockers ?? []).filter((b) => b.state === 'OPEN')
    const issue = iss(1, [], {
      status: c.status ?? '',
      label,
      needs_label: NEEDS_LABELS.some((k) => c.labels?.includes(k)),
      open_question: c.open_question ?? false,
      blocked_by: open.length > 0 ? open.map((b) => ({ repo: R, issue: b.issue })) : null,
    })
    const runs: Run[] = c.run ? [{ repo: R, issue: 1, state: c.run, phase: 'sdd' }] : []
    expect(issueState(issue, runs, R)).toEqual({
      state: c.want.state,
      needsHuman: c.want.needs_human ?? false,
    })
  })
})

describe('issueState / isUphill', () => {
  const run = (state: string, phase: string): Run => ({ repo: R, issue: 1, state, phase })
  it('a done run does not make an issue active', () => {
    expect(issueState(iss(1), [run('done', 'sdd')], R).state).toBe('ready')
  })
  it('a run of another issue does not make an issue active', () => {
    const other: Run = { repo: R, issue: 2, state: 'open', phase: 'sdd' }
    expect(issueState(iss(1), [other], R).state).toBe('ready')
  })
  it('unknown phase is downhill, spec-review is uphill', () => {
    expect(isUphill(undefined)).toBe(false)
    expect(isUphill(run('open', 'onbekend'))).toBe(false)
    expect(isUphill(run('open', 'spec-review'))).toBe(true)
  })
})

describe('isShaping', () => {
  it.each(['Status: shaping', '  status: SHAPING  ', 'intro\nStatus: shaping\nrest'])(
    'true for %j',
    (d) => expect(isShaping(d, 3)).toBe(true),
  )
  it.each(['Status: shaping up', 'status: shaped'])('false for %j', (d) =>
    expect(isShaping(d, 3)).toBe(false),
  )
  it('empty milestone always shaping', () => {
    expect(isShaping('', 0)).toBe(true)
    expect(isShaping('', 3)).toBe(false)
  })
})

describe('milestoneRows', () => {
  const m = (number: number, done: number, open = 99, description = '') => ({
    number,
    title: `M${number}`,
    done,
    open,
    description,
    closed: [],
  })
  const ref = (number: number) => ({ number, title: `M${number}`, state: 'open' })

  it('percent is done/(done+delivered open); search Open is ignored', () => {
    const rs = [repo(R, [iss(1, [], { milestone: ref(1) }), iss(2, [], { milestone: ref(1) })], [m(1, 2)])]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row).toMatchObject({ open: 2, done: 2, total: 4, percent: 0.5, empty: false })
  })

  it('outside holds transitive external blockers, two layers deep, not own issues', () => {
    const rs = [
      repo(
        R,
        [
          iss(1, [[R, 2]], { milestone: ref(1) }),
          iss(2, [[R, 3]]),
          iss(3, [['z/q', 9]]),
          iss(4, [], { milestone: ref(1) }),
        ],
        [m(1, 0)],
      ),
    ]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row.outside.map((x) => x.key)).toEqual([`${R}#2`, `${R}#3`, 'z/q#9'])
    expect(row.blocks.filter((b) => b.repo === R).map((b) => b.nr)).toEqual([4, 1])
  })

  it('blocks keep a cycle together by number and put a blocker before what it blocks', () => {
    const rs = [
      repo(
        R,
        [
          iss(5, [[R, 7]], { milestone: ref(1) }),
          iss(6, [], { milestone: ref(1) }),
          iss(7, [[R, 5]], { milestone: ref(1) }),
          iss(8, [[R, 6]], { milestone: ref(1) }),
        ],
        [m(1, 0)],
      ),
    ]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    const nums = row.blocks.map((b) => b.nr)
    expect(nums.indexOf(6)).toBeLessThan(nums.indexOf(8))
    expect(Math.abs(nums.indexOf(5) - nums.indexOf(7))).toBe(1)
    expect(nums.indexOf(5)).toBeLessThan(nums.indexOf(7))
  })

  it('gives an In progress issue the active state and a plain one ready', () => {
    const rs = [
      repo(R, [iss(1, [], { milestone: ref(1), status: 'In progress' }), iss(2, [], { milestone: ref(1) })], [m(1, 0)]),
    ]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row.blocks.map((b) => [b.nr, b.state])).toEqual([[1, 'active'], [2, 'ready']])
  })

  it('empty milestone (also only not-planned) is shaping with percent 0', () => {
    const rs = [repo(R, [], [m(1, 0, 0)])]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row).toMatchObject({ empty: true, percent: 0, shaping: true })
  })

  it('shaping marker on a milestone with issues', () => {
    const rs = [repo(R, [iss(1, [], { milestone: ref(1) })], [m(1, 1, 1, 'Status: shaping')])]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row.shaping).toBe(true)
  })

  it('sortMilestoneRows puts a shaped milestone above one marked shaping (same flag as the border)', () => {
    const rs = [
      repo(
        R,
        [iss(1, [], { milestone: ref(1) }), iss(2, [], { milestone: ref(2) })],
        [m(1, 5, 1, 'Status: shaping'), m(2, 0, 1)],
      ),
    ]
    const rows = milestoneRows(rs, buildGraph(rs, []))
    expect(sortMilestoneRows(rows).map((r) => r.number)).toEqual([2, 1])
  })
})

describe('milestoneRows blocks', () => {
  const ref = { number: 1, title: 'M1', state: 'open' }
  const closed = [
    { number: 30, title: 'c30', url: 'u30' },
    { number: 20, title: 'c20', url: 'u20' },
  ]
  const ms = (extra: Partial<NonNullable<Repo['milestones']>[number]> = {}) => ({
    number: 1,
    title: 'M1',
    done: 2,
    open: 99,
    description: '',
    closed,
    ...extra,
  })
  const mk = (issues: Issue[], milestone = ms()) => {
    const rs = [repo(R, issues, [milestone])]
    return milestoneRows(rs, buildGraph(rs, []))[0]
  }

  it('groups done, active, ready, needs_human, blocked in that order, by work order inside a group', () => {
    const row = mk([
      iss(1, [[R, 9]], { milestone: ref }),
      iss(2, [], { milestone: ref, label: 'needs_decision' }),
      iss(3, [], { milestone: ref }),
      iss(4, [], { milestone: ref, status: 'In progress', open_question: true }),
      iss(5, [[R, 3]], { milestone: ref }),
      iss(6, [], { milestone: ref, status: 'Controller' }),
      iss(9, [], { milestone: ref }),
    ])
    expect(row.blocks.map((b) => [b.nr, b.state, b.needsHuman])).toEqual([
      [20, 'done', false],
      [30, 'done', false],
      [4, 'active', true],
      [6, 'active', false],
      [3, 'ready', false],
      [9, 'ready', false],
      [2, 'needs_human', false],
      [1, 'blocked', false],
      [5, 'blocked', false],
    ])
    expect(row.blocks[0]).toEqual({
      nr: 20,
      repo: R,
      title: 'c20',
      url: 'u20',
      state: 'done',
      needsHuman: false,
    })
  })

  it('orders a group by dependency depth before number', () => {
    const row = mk([
      iss(1, [[R, 5]], { milestone: ref, label: 'needs_decision' }),
      iss(5, [], { milestone: ref, label: 'needs_decision' }),
    ])
    expect(row.blocks.filter((b) => b.state === 'needs_human').map((b) => b.nr)).toEqual([5, 1])
  })

  it('an outside blocker without issue data is in outside, not in blocks', () => {
    const row = mk([iss(1, [['z/q', 7]], { milestone: ref })])
    expect(row.blocks.filter((b) => b.state !== 'done').map((b) => b.nr)).toEqual([1])
    expect(row.outside).toMatchObject([{ repo: 'z/q', number: 7, external: true }])
  })

  it('no milestone issues and no closed gives shaping with percent 0, not NaN', () => {
    const row = mk([], ms({ done: 0, closed: null }))
    expect(row).toMatchObject({ blocks: [], shaping: true, percent: 0 })
  })
})

describe('sortMilestoneRows', () => {
  const row = (number: number, open: number, done: number, shaping: boolean, repo = R) =>
    ({
      repo,
      number,
      open,
      done,
      total: open + done,
      shaping,
      empty: open === 0 && done === 0,
      percent: open + done === 0 ? 0 : done / (open + done),
    }) as Parameters<typeof sortMilestoneRows>[0][number]

  it('one milestone per group, inserted in reverse, comes back as groups 1 to 4', () => {
    const shaped = row(1, 1, 3, false)
    const withDone = row(2, 2, 1, true)
    const noneDone = row(3, 4, 0, true)
    const empty = row(4, 0, 0, true)
    const sorted = sortMilestoneRows([empty, noneDone, withDone, shaped])
    expect(sorted.map((r) => r.number)).toEqual([1, 2, 3, 4])
  })

  it('group 1: highest percent first, equal percent fewest open first', () => {
    const sorted = sortMilestoneRows([row(1, 4, 4, false), row(2, 2, 2, false), row(3, 1, 9, false), row(4, 5, 5, false)])
    expect(sorted.map((r) => r.number)).toEqual([3, 2, 1, 4])
  })

  it('group 2: most finished first', () => {
    const sorted = sortMilestoneRows([row(1, 9, 1, true), row(2, 0, 5, true), row(3, 1, 3, true)])
    expect(sorted.map((r) => r.number)).toEqual([2, 3, 1])
  })

  it('group 3: most issues first', () => {
    const sorted = sortMilestoneRows([row(1, 1, 0, true), row(2, 6, 0, true), row(3, 3, 0, true)])
    expect(sorted.map((r) => r.number)).toEqual([2, 3, 1])
  })

  it('ties end on repo, then number', () => {
    const sorted = sortMilestoneRows([row(2, 0, 0, true, 'b/x'), row(9, 0, 0, true, 'a/x'), row(1, 0, 0, true, 'b/x')])
    expect(sorted.map((r) => `${r.repo}#${r.number}`)).toEqual(['a/x#9', 'b/x#1', 'b/x#2'])
  })
})

describe('withoutMilestone', () => {
  it('lists closed-milestone issues and sorts relation first, then number desc', () => {
    const rs = [
      repo(R, [
        iss(10),
        iss(20),
        iss(5, [[R, 7]]),
        iss(7),
        iss(30, [], { milestone: { number: 2, title: 'Old', state: 'closed' } }),
        iss(40, [], { milestone: { number: 3, title: 'Live', state: 'open' } }),
      ]),
    ]
    const rows = withoutMilestone(rs, buildGraph(rs, []))
    expect(rows.map((r) => r.node.number)).toEqual([7, 5, 30, 20, 10])
    expect(rows[0]).toMatchObject({ relation: 'blocks', relatedNumber: 5 })
    expect(rows[1]).toMatchObject({ relation: 'waits', relatedNumber: 7 })
    expect(rows[2].closedMilestone).toBe('Old')
    expect(rows[3].closedMilestone).toBeNull()
  })
})

describe('milestone rank and priority order', () => {
  const mrow = (repo: string, number: number, rank: number | null) =>
    ({ repo, number, rank }) as Parameters<typeof priorityOrder>[0][number]
  const ids = (rows: Array<{ repo: string; number: number }>) => rows.map((r) => `${r.repo}${r.number}`)

  it('puts place 1 of every repo first, then place 2, with unranked below', () => {
    // Input is in computed order.
    const rows = [
      mrow('A', 10, 2),
      mrow('B', 20, null),
      mrow('A', 11, 1),
      mrow('B', 21, 1),
      mrow('A', 12, null),
    ]
    expect(ids(priorityOrder(rows))).toEqual(['A11', 'B21', 'A10', 'B20', 'A12'])
  })

  it('keeps computed order for equal ranks within a repo', () => {
    const rows = [mrow('A', 3, 1), mrow('A', 1, 1), mrow('A', 2, 1)]
    expect(ids(priorityOrder(rows))).toEqual(['A3', 'A1', 'A2'])
  })

  it('does not mutate its input', () => {
    const rows = [mrow('A', 1, 2), mrow('A', 2, 1)]
    priorityOrder(rows)
    expect(ids(rows)).toEqual(['A1', 'A2'])
  })

  it('placeInRepo counts ranked milestones of that repo only', () => {
    const rows = [mrow('A', 1, 5), mrow('B', 2, 1), mrow('A', 3, 2), mrow('A', 4, null)]
    expect(placeInRepo(rows, 'A', 3)).toBe(1)
    expect(placeInRepo(rows, 'A', 1)).toBe(2)
    expect(placeInRepo(rows, 'B', 2)).toBe(1)
    expect(placeInRepo(rows, 'A', 4)).toBeNull()
    expect(placeInRepo(rows, 'A', 99)).toBeNull()
  })

  it('milestoneRows reads the rank from the description', () => {
    const repos = [
      repo(R, [], [
        { number: 1, title: 'one', open: 0, done: 0, closed: null, description: 'x\nPriority: 4' },
        { number: 2, title: 'two', open: 0, done: 0, closed: null, description: 'none' },
      ]),
    ]
    const rows = milestoneRows(repos, buildGraph(repos, []))
    expect(rows.map((r) => r.rank)).toEqual([4, null])
  })
})
