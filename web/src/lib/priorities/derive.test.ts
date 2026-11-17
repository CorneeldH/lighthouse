import { describe, expect, it } from 'vitest'
import type { Issue, Repo, Run } from '../../api/lighthouse'
import {
  buildGraph,
  isActive,
  isShaping,
  isUphill,
  milestoneRows,
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

describe('isActive / isUphill', () => {
  const run = (state: string, phase: string): Run => ({ repo: R, issue: 1, state, phase })
  it('parked run is active and downhill in executing-plans', () => {
    const r = run('parked', 'executing-plans')
    expect(isActive(iss(1), [r], R)).toBe(true)
    expect(isUphill(r)).toBe(false)
  })
  it('In progress without run is active and downhill', () => {
    expect(isActive(iss(1, [], { status: 'In progress' }), [], R)).toBe(true)
    expect(isUphill(undefined)).toBe(false)
  })
  it('unknown phase is downhill, spec-review is uphill', () => {
    expect(isUphill(run('open', 'onbekend'))).toBe(false)
    expect(isUphill(run('open', 'spec-review'))).toBe(true)
  })
  it('To do without run is not active; a closed run does not count', () => {
    expect(isActive(iss(1), [], R)).toBe(false)
    expect(isActive(iss(1), [run('done', 'sdd')], R)).toBe(false)
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
    expect(row.order.map((x) => x.number)).toEqual([4, 1])
  })

  it('order is topological and keeps a cycle together by number', () => {
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
    const nums = row.order.map((x) => x.number)
    expect(nums.indexOf(6)).toBeLessThan(nums.indexOf(8))
    expect(Math.abs(nums.indexOf(5) - nums.indexOf(7))).toBe(1)
    expect(nums.indexOf(5)).toBeLessThan(nums.indexOf(7))
  })

  it('splits active and todo', () => {
    const rs = [
      repo(R, [iss(1, [], { milestone: ref(1), status: 'In progress' }), iss(2, [], { milestone: ref(1) })], [m(1, 0)]),
    ]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row.active.map((x) => x.number)).toEqual([1])
    expect(row.todo.map((x) => x.number)).toEqual([2])
  })

  it('empty milestone (also only not-planned) is shaping with null percent', () => {
    const rs = [repo(R, [], [m(1, 0, 0)])]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row).toMatchObject({ empty: true, percent: null, shaping: true })
  })

  it('shaping marker on a milestone with issues', () => {
    const rs = [repo(R, [iss(1, [], { milestone: ref(1) })], [m(1, 1, 1, 'Status: shaping')])]
    const [row] = milestoneRows(rs, buildGraph(rs, []))
    expect(row.shaping).toBe(true)
  })
})

describe('sortMilestoneRows', () => {
  const row = (number: number, open: number, done: number) =>
    ({
      repo: R,
      number,
      open,
      done,
      empty: open === 0 && done === 0,
      percent: open + done === 0 ? null : done / (open + done),
    }) as Parameters<typeof sortMilestoneRows>[0][number]

  it('fewest open first, ties by highest percent, empty last', () => {
    const sorted = sortMilestoneRows([row(1, 0, 0), row(2, 3, 1), row(3, 1, 0), row(4, 1, 5), row(5, 3, 9)])
    expect(sorted.map((r) => r.number)).toEqual([4, 3, 5, 2, 1])
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
