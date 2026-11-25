import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Issue, Milestone, Repo, Run } from '@/api/lighthouse'
import { buildGraph, type MilestoneRow } from '@/lib/priorities/derive'
import { assignColors } from '@/lib/priorities/palette'
import { MilestonesView } from './MilestonesView'

// jsdom has no layout: the bar reports `barWidth`, and the ResizeObserver fires once on observe.
let barWidth = 1000
beforeEach(() => {
  barWidth = 1000
  vi.stubGlobal(
    'ResizeObserver',
    class {
      cb: () => void
      constructor(cb: () => void) {
        this.cb = cb
      }
      observe() {
        this.cb()
      }
      disconnect() {}
    },
  )
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    const w = this.getAttribute('data-testid') === 'bar' ? barWidth : 0
    return { width: w, height: 18, top: 0, left: 0, right: w, bottom: 18, x: 0, y: 0, toJSON: () => ({}) }
  })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const A = 'a/x'
const iss = (
  number: number,
  ms: number | null,
  by: Array<[string, number]> = [],
  extra: Partial<Issue> = {},
): Issue => ({
  number,
  title: `title-${number}`,
  url: `https://forge/${number}`,
  status: 'To do',
  label: '',
  open_question: false,
  needs_label: false,
  prio_weight: 0,
  factor: 0,
  blocked_by: by.length ? by.map(([r, i]) => ({ repo: r, issue: i })) : null,
  ...(ms === null ? {} : { milestone: { number: ms, state: 'open', title: `M${ms}`, url: `u-m${ms}` } }),
  ...extra,
})
const ms = (number: number, done: number, open: number, description = ''): Milestone => ({
  number,
  title: `M${number}`,
  done,
  open,
  description,
  url: `u-m${number}`,
  closed: [],
})
const repo = (slug: string, issues: Issue[], milestones: Milestone[]): Repo => ({ slug, issues, milestones })

function view(repos: Repo[], selected: string[] = [A], runs: Run[] = []) {
  const graph = buildGraph(repos, runs)
  const colors = assignColors(
    repos
      .filter((r) => selected.includes(r.slug))
      .flatMap((r) => (r.milestones ?? []).map((m) => ({ repo: r.slug, number: m.number }))),
  )
  return render(<MilestonesView graph={graph} repos={repos} selected={new Set(selected)} colors={colors} />)
}
const row = (key: string) => screen.getByTestId(`ms-${key}`)
const blocks = (key: string) => within(row(key)).getAllByTestId('block')

describe('MilestonesView', () => {
  it('counts done, active and not yet, and names outside blockers only when there are some', () => {
    const r = repo(
      A,
      [iss(1, 1, [], { status: 'In progress' }), iss(2, 1), iss(3, null), iss(4, 2, [[A, 3]])],
      [ms(1, 2, 2), ms(2, 0, 1)],
    )
    view([r])
    expect(within(row(`${A}#m1`)).getByText('2 done · 1 active · 1 not yet')).toBeTruthy()
    expect(within(row(`${A}#m2`)).getByText('0 done · 0 active · 1 not yet · 1 outside blocker')).toBeTruthy()
  })

  it('pluralises outside blockers', () => {
    const r = repo(A, [iss(1, null), iss(2, null), iss(3, 1, [[A, 1], [A, 2]])], [ms(1, 0, 1)])
    view([r])
    expect(within(row(`${A}#m1`)).getByText(/· 2 outside blockers$/)).toBeTruthy()
  })

  it('draws one block per issue grouped by state: done, needs human, active, ready, needs shaping, blocked', () => {
    const r = repo(
      A,
      [
        iss(1, 1, [[A, 2]]),
        iss(2, 1, [], { label: 'needs_shaping' }),
        iss(3, 1),
        iss(4, 1, [], { status: 'In progress', open_question: true }),
        iss(5, 1, [], { status: 'In progress' }),
      ],
      [{ ...ms(1, 1, 5), closed: [{ number: 9, title: 'c9', url: 'u9' }] }],
    )
    view([r])
    expect(
      blocks(`${A}#m1`).map((b) => [b.getAttribute('data-state'), b.title]),
    ).toEqual([
      ['done', '9 c9'],
      ['needs_human', '4 title-4'],
      ['active', '5 title-5'],
      ['ready', '3 title-3'],
      ['needs_shaping', '2 title-2'],
      ['blocked', '1 title-1'],
    ])
  })

  it('draws a needs_shaping block as a dashed shape and a needs_human block as stripes', () => {
    view([
      repo(A, [iss(1, 1, [], { label: 'needs_shaping' }), iss(2, 1, [], { status: 'In progress', open_question: true })], [ms(1, 0, 2)]),
    ])
    const [human, shaping] = blocks(`${A}#m1`)
    expect(human.getAttribute('data-state')).toBe('needs_human')
    expect(human.className).toContain('lh-stripe')
    expect(shaping.getAttribute('data-state')).toBe('needs_shaping')
    expect(shaping.style.outline).toBe('1.5px dashed var(--foreground)')
    expect(blocks(`${A}#m1`).every((x) => !x.hasAttribute('data-needs-human'))).toBe(true)
  })

  it('opens the issue url in a new tab on click', () => {
    view([repo(A, [iss(7, 1)], [ms(1, 0, 1)])])
    const b = blocks(`${A}#m1`)[0]
    expect(b.tagName).toBe('A')
    expect(b.getAttribute('href')).toBe('https://forge/7')
    expect(b.getAttribute('target')).toBe('_blank')
  })

  it('shows the number in a block from MIN_LABEL_WIDTH on, and only the hover title below it', () => {
    // Two blocks with a 2 px gap: (58 - 2) / 2 = 28 px each, (57 - 2) / 2 = 27.5 px each.
    const r = repo(A, [iss(1, 1), iss(2, 1)], [ms(1, 0, 2)])
    barWidth = 58
    view([r])
    expect(blocks(`${A}#m1`).map((b) => b.textContent)).toEqual(['1', '2'])
    cleanup()
    barWidth = 57
    view([r])
    const narrow = blocks(`${A}#m1`)
    expect(narrow.map((b) => b.textContent)).toEqual(['', ''])
    expect(narrow[0].getAttribute('title')).toBe('1 title-1')
  })

  it('draws an outside blocker solid in the colour of its own milestone, in the fixed column', () => {
    const r = repo(
      A,
      [iss(1, 2), iss(2, 1, [[A, 1]]), iss(3, 2)],
      [ms(1, 0, 1), ms(2, 0, 2)],
    )
    view([r])
    const withOut = within(row(`${A}#m1`))
    const out = withOut.getByTestId('outside-col').querySelectorAll('[data-testid="outside"]')
    expect(out).toHaveLength(1)
    expect(out[0].textContent).toBe('1')
    const own = within(row(`${A}#m2`)).getByTestId('swatch').style.background
    expect((out[0] as HTMLElement).style.border).toBe(`1.5px solid ${own}`)
    // The outside blocker is not drawn twice: the bar holds only the milestone's own issue.
    expect(blocks(`${A}#m1`).map((b) => b.title)).toEqual(['2 title-2'])
    // Same fixed column on a row without outside blockers keeps the bars aligned.
    expect(within(row(`${A}#m2`)).getByTestId('outside-col').className).toBe(
      withOut.getByTestId('outside-col').className,
    )
    expect(within(row(`${A}#m2`)).queryAllByTestId('outside')).toHaveLength(0)
  })

  it('an outside blocker without issue data is drawn as a ready block', () => {
    view([repo(A, [iss(1, 1, [['z/q', 7]])], [ms(1, 0, 1)])])
    const out = within(row(`${A}#m1`)).getByTestId('outside')
    expect(out.getAttribute('data-state')).toBe('ready')
    expect(out.hasAttribute('data-needs-human')).toBe(false)
    // Not drawn twice: the bar holds only the milestone's own issue.
    expect(blocks(`${A}#m1`).map((b) => b.title)).toEqual(['1 title-1'])
  })

  it('caps outside blockers and folds the rest into +N in the same column', () => {
    const blockers = [1, 2, 3, 4, 5, 6, 7]
    const r = repo(
      A,
      [...blockers.map((n) => iss(n, null)), iss(10, 1, blockers.map((n) => [A, n] as [string, number])), iss(11, 2)],
      [ms(1, 0, 1), ms(2, 0, 1)],
    )
    view([r])
    const col = within(row(`${A}#m1`)).getByTestId('outside-col')
    expect(col.querySelectorAll('[data-testid="outside"]')).toHaveLength(2)
    const more = within(col).getByTestId('outside-more')
    expect(more.textContent).toBe('+5')
    expect(more.getAttribute('title')).toContain('3 title-3')
    expect(more.getAttribute('title')).toContain('7 title-7')
    expect(within(row(`${A}#m2`)).getByTestId('outside-col').className).toBe(col.className)
  })

  it('lists the open issues after "to do:" in block order, without outside blockers', () => {
    const r = repo(
      A,
      [iss(1, null), iss(2, 1, [[A, 1]]), iss(3, 1), iss(4, 1, [[A, 3]])],
      [ms(1, 0, 3)],
    )
    view([r])
    const list = within(row(`${A}#m1`)).getByTestId('todo-list')
    expect(list.textContent).toBe('to do:324')
  })

  it('shows the percentage and the number of own open issues on the right', () => {
    const r = repo(A, [iss(1, null), iss(2, 1, [[A, 1]]), iss(3, 1)], [ms(1, 2, 2)])
    view([r])
    const right = within(row(`${A}#m1`))
    expect(right.getByText('50%')).toBeTruthy()
    expect(right.getByText('2 to do')).toBeTruthy()
  })

  it('marks a shaping milestone with one grey dashed border round the bar and a grey provisional percentage', () => {
    view([repo(A, [iss(1, 1)], [ms(1, 1, 1, 'Status: shaping')])])
    const r = within(row(`${A}#m1`))
    expect(r.getByTestId('bar').getAttribute('data-shaping')).toBe('true')
    expect(r.getByText('50%').className).toContain('text-muted')
    expect(r.getByText('provisional')).toBeTruthy()
    expect(r.queryByText('no issues yet')).toBeNull()
    expect(blocks(`${A}#m1`)).toHaveLength(1)
  })

  it('shows no issues yet in the empty bar of a milestone without issues', () => {
    view([repo(A, [], [ms(1, 0, 0)])])
    const r = within(row(`${A}#m1`))
    expect(r.getByText('no issues yet')).toBeTruthy()
    expect(r.getByTestId('bar').getAttribute('data-shaping')).toBe('true')
    expect(r.queryAllByTestId('block')).toHaveLength(0)
    expect(r.getByText('provisional')).toBeTruthy()
  })

  it('does not mark a shaped milestone', () => {
    view([repo(A, [iss(1, 1)], [ms(1, 1, 1)])])
    const r = within(row(`${A}#m1`))
    expect(r.getByTestId('bar').getAttribute('data-shaping')).toBe('false')
    expect(r.queryByText('provisional')).toBeNull()
  })

  it('sorts like sortMilestoneRows: shaped first, empty last', () => {
    const r = repo(
      A,
      [iss(1, 1), iss(2, 1), iss(3, 2)],
      [ms(1, 0, 2), ms(2, 0, 1), ms(3, 0, 0)],
    )
    view([r])
    const order = screen.getAllByTestId(/^ms-/).map((e) => e.getAttribute('data-testid'))
    expect(order).toEqual([`ms-${A}#m2`, `ms-${A}#m1`, `ms-${A}#m3`])
  })

  it('shows only rows of selected repos, and colours the swatch from the colors prop', () => {
    const rs = [
      repo(A, [iss(1, 1)], [ms(1, 0, 1)]),
      repo('a/y', [iss(1, 1)], [ms(1, 0, 1)]),
    ]
    const graph = buildGraph(rs, [])
    const colors = new Map([[`${A}#m1`, 'rgb(1, 2, 3)']])
    render(<MilestonesView graph={graph} repos={rs} selected={new Set([A])} colors={colors} />)
    expect(screen.queryByTestId('ms-a/y#m1')).toBeNull()
    expect(within(row(`${A}#m1`)).getByTestId('swatch').getAttribute('style')).toContain('rgb(1, 2, 3)')
    expect(screen.getByTestId('view-milestones')).toBeTruthy()
  })

  describe('priority order', () => {
    const B = 'b/y'
    // Computed order is m3 (1 open), m1 (2 open), m2 (3 open), m4 (empty); ranks put m2 first.
    const ranked = () =>
      repo(
        A,
        [iss(1, 1), iss(2, 1), iss(3, 2), iss(4, 2), iss(5, 2), iss(6, 3)],
        [ms(1, 0, 2, 'Priority: 2'), ms(2, 0, 3, 'Pitch\nPriority: 1'), ms(3, 0, 1), ms(4, 0, 0)],
      )
    function prio(
      repos: Repo[],
      extra: { onMove?: (row: MilestoneRow, dir: 'up' | 'down') => void; pendingRepos?: Set<string>; order?: 'computed' | 'priority' } = {},
      selected: string[] = [A],
    ) {
      const graph = buildGraph(repos, [])
      return render(
        <MilestonesView
          graph={graph}
          repos={repos}
          selected={new Set(selected)}
          colors={new Map()}
          order={extra.order ?? 'priority'}
          onMove={extra.onMove ?? vi.fn()}
          pendingRepos={extra.pendingRepos ?? new Set()}
        />,
      )
    }
    const btn = (key: string, name: 'Up' | 'Down') =>
      within(row(key)).getByRole('button', { name: new RegExp(`${name}$`, 'i') }) as HTMLButtonElement
    const order = () => screen.getAllByTestId(/^ms-/).map((e) => e.getAttribute('data-testid'))

    it('lists ranked milestones first by place, unranked below in computed order', () => {
      prio([ranked()])
      expect(order()).toEqual([`ms-${A}#m2`, `ms-${A}#m1`, `ms-${A}#m3`, `ms-${A}#m4`])
    })

    it('keeps the computed order in Computed mode, without buttons or place', () => {
      prio([ranked()], { order: 'computed' })
      expect(order()).toEqual([`ms-${A}#m3`, `ms-${A}#m1`, `ms-${A}#m2`, `ms-${A}#m4`])
      expect(screen.queryAllByRole('button', { name: /up|down/i })).toHaveLength(0)
      expect(screen.queryAllByTestId('place')).toHaveLength(0)
    })

    it('shows the place in the repo next to the title, and no number without a rank', () => {
      prio([ranked()])
      expect(within(row(`${A}#m2`)).getByTestId('place').textContent).toBe('1')
      expect(within(row(`${A}#m1`)).getByTestId('place').textContent).toBe('2')
      expect(within(row(`${A}#m3`)).queryByTestId('place')).toBeNull()
      // A screen reader hears "place 1", not a bare number.
      expect(within(row(`${A}#m2`)).getByText(/place 1/).textContent).toBe(', place 1')
    })

    it('disables up on place 1, down on the last ranked and down without a rank', () => {
      prio([ranked()])
      expect(btn(`${A}#m2`, 'Up').disabled).toBe(true)
      expect(btn(`${A}#m2`, 'Down').disabled).toBe(false)
      expect(btn(`${A}#m1`, 'Up').disabled).toBe(false)
      expect(btn(`${A}#m1`, 'Down').disabled).toBe(true)
      expect(btn(`${A}#m3`, 'Up').disabled).toBe(false)
      expect(btn(`${A}#m3`, 'Down').disabled).toBe(true)
    })

    it('gives every button a name with its milestone title', () => {
      prio([ranked()])
      expect(within(row(`${A}#m1`)).getByRole('button', { name: 'M1 up' })).toBeTruthy()
      expect(within(row(`${A}#m2`)).getByRole('button', { name: 'M2 down' })).toBeTruthy()
    })

    it('calls onMove with the row and the direction of the clicked button', () => {
      const onMove = vi.fn<(row: MilestoneRow, dir: 'up' | 'down') => void>()
      prio([ranked()], { onMove })
      fireEvent.click(btn(`${A}#m1`, 'Up'))
      fireEvent.click(btn(`${A}#m2`, 'Down'))
      expect(onMove.mock.calls.map(([r, d]) => [r.repo, r.number, d])).toEqual([
        [A, 1, 'up'],
        [A, 2, 'down'],
      ])
    })

    it('disables every button of a repo with a running request, not those of another repo', () => {
      const other = repo(B, [], [ms(7, 1, 0, 'Priority: 1'), ms(8, 1, 0, 'Priority: 2')])
      prio([ranked(), other], { pendingRepos: new Set([A]) }, [A, B])
      for (const k of [`${A}#m1`, `${A}#m2`, `${A}#m3`]) {
        expect(btn(k, 'Up').disabled).toBe(true)
        expect(btn(k, 'Down').disabled).toBe(true)
      }
      expect(btn(`${B}#m8`, 'Up').disabled).toBe(false)
      expect(btn(`${B}#m7`, 'Down').disabled).toBe(false)
    })

    it('numbers places per repo when repos are mixed', () => {
      const other = repo(B, [], [ms(7, 1, 0, 'Priority: 5')])
      prio([ranked(), other], {}, [A, B])
      expect(within(row(`${B}#m7`)).getByTestId('place').textContent).toBe('1')
      expect(order()).toEqual([`ms-${B}#m7`, `ms-${A}#m2`, `ms-${A}#m1`, `ms-${A}#m3`, `ms-${A}#m4`])
    })
  })
})
