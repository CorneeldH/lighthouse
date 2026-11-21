import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Issue, Milestone, Repo, Run } from '@/api/lighthouse'
import { buildGraph, type MilestoneRow } from '@/lib/priorities/derive'
import { assignColors } from '@/lib/priorities/palette'
import { MilestonesView } from './MilestonesView'

afterEach(cleanup)

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
  blocked_by: by.length ? by.map(([r, i]) => ({ repo: r, issue: i })) : null,
  ...(ms === null ? {} : { milestone: { number: ms, state: 'open', title: `M${ms}` } }),
  ...extra,
})
const ms = (number: number, done: number, open: number, description = ''): Milestone => ({
  number,
  title: `M${number}`,
  done,
  open,
  description,
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
const segs = (key: string) =>
  within(row(key))
    .getAllByTestId('seg')
    .map((s) => s.getAttribute('data-state'))

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

  it('draws one segment per issue, done then active then not yet', () => {
    const r = repo(
      A,
      [iss(1, 1), iss(2, 1, [], { status: 'In progress' }), iss(3, 1)],
      [ms(1, 2, 3)],
    )
    view([r])
    expect(segs(`${A}#m1`)).toEqual(['done', 'done', 'active', 'todo', 'todo'])
  })

  it('hatches an active segment that is uphill, not one that is downhill', () => {
    const r = repo(
      A,
      [iss(1, 1), iss(2, 1)],
      [ms(1, 0, 2)],
    )
    const runs: Run[] = [
      { repo: A, issue: 1, phase: 'brainstorming', state: 'open' },
      { repo: A, issue: 2, phase: 'executing', state: 'open' },
    ]
    view([r], [A], runs)
    const s = within(row(`${A}#m1`)).getAllByTestId('seg')
    expect(s.map((x) => x.getAttribute('data-hatched'))).toEqual(['true', 'false'])
  })

  it('shows number and title of an open segment on hover, and only "done" on a done one', () => {
    view([repo(A, [iss(1, 1)], [ms(1, 1, 1)])])
    const [done, open] = within(row(`${A}#m1`)).getAllByTestId('seg')
    expect(done.getAttribute('title')).toBe('done')
    expect(open.getAttribute('title')).toBe('#1 title-1')
  })

  it('puts outside blockers as dashed blocks in a fixed column before the bar', () => {
    const r = repo(
      A,
      [iss(1, null), iss(2, 1, [[A, 1]]), iss(3, 2)],
      [ms(1, 0, 1), ms(2, 0, 1)],
    )
    view([r])
    const withOut = within(row(`${A}#m1`))
    const blk = withOut.getByTestId('outside-col').querySelectorAll('[data-testid="outside"]')
    expect(blk).toHaveLength(1)
    expect(blk[0].className).toContain('border-dashed')
    expect(blk[0].textContent).toBe('1')
    // Same fixed column on a row without outside blockers keeps the bars aligned.
    expect(within(row(`${A}#m2`)).getByTestId('outside-col').className).toBe(
      withOut.getByTestId('outside-col').className,
    )
    expect(within(row(`${A}#m2`)).queryAllByTestId('outside')).toHaveLength(0)
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
    expect(more.getAttribute('title')).toContain('#3 title-3')
    expect(more.getAttribute('title')).toContain('#7 title-7')
    expect(within(row(`${A}#m2`)).getByTestId('outside-col').className).toBe(col.className)
  })

  it('draws active segments and active outside blockers with the --lh-yellow token', () => {
    const r = repo(
      A,
      [iss(1, null, [], { status: 'In progress' }), iss(2, 1, [[A, 1]], { status: 'In progress' })],
      [ms(1, 0, 1)],
    )
    view([r])
    const style = (e: HTMLElement) => e.getAttribute('style') ?? ''
    const active = within(row(`${A}#m1`)).getAllByTestId('seg').find((s) => s.getAttribute('data-state') === 'active')!
    expect(style(active)).toContain('var(--lh-yellow)')
    expect(style(active)).not.toMatch(/#[0-9a-f]{3,6}/i)
    expect(style(within(row(`${A}#m1`)).getByTestId('outside'))).toContain('var(--lh-yellow)')
  })

  it('lists the open issues after "to do:" in order, without outside blockers', () => {
    const r = repo(
      A,
      [iss(1, null), iss(2, 1, [[A, 1]]), iss(3, 1), iss(4, 1, [[A, 3]])],
      [ms(1, 0, 3)],
    )
    view([r])
    const list = within(row(`${A}#m1`)).getByTestId('todo-list')
    expect(list.textContent).toBe('to do:#3#2#4')
  })

  it('shows the percentage and the number of own open issues on the right', () => {
    const r = repo(A, [iss(1, null), iss(2, 1, [[A, 1]]), iss(3, 1)], [ms(1, 2, 2)])
    view([r])
    const right = within(row(`${A}#m1`))
    expect(right.getByText('50%')).toBeTruthy()
    expect(right.getByText('2 to do')).toBeTruthy()
  })

  it('shows a shaping milestone without issues as a hatched grey bar and no percentage', () => {
    view([repo(A, [], [ms(1, 0, 0, 'Status: shaping')])])
    const r = within(row(`${A}#m1`))
    expect(r.getByText('Not shaped yet — the percentage says nothing')).toBeTruthy()
    expect(r.getByTestId('unshaped').getAttribute('data-hatched')).toBe('true')
    expect(r.getByText('—')).toBeTruthy()
    expect(r.getByText('shape first')).toBeTruthy()
    expect(r.queryByText(/%/)).toBeNull()
    expect(r.queryAllByTestId('seg')).toHaveLength(0)
  })

  it('shows a shaping milestone with issues as provisional, with a dashed layer over the bar', () => {
    view([repo(A, [iss(1, 1)], [ms(1, 1, 1, 'Status: shaping')])])
    const r = within(row(`${A}#m1`))
    expect(r.getByText('50%').className).toContain('text-muted')
    expect(r.getByText('provisional · 1 to do')).toBeTruthy()
    const layer = r.getByTestId('shaping-layer')
    expect(layer.className).toContain('border-dashed')
    expect(layer.className).toContain('pointer-events-none')
    expect(layer.getAttribute('title')).toBe('still in shaping — the list can still grow')
    expect(r.getAllByTestId('seg')).toHaveLength(2)
  })

  it('treats a milestone with only pull requests as empty: a dash, never 100%', () => {
    view([repo(A, [], [ms(1, 0, 0)])])
    const r = within(row(`${A}#m1`))
    expect(r.getByText('—')).toBeTruthy()
    expect(r.queryByText('100%')).toBeNull()
    expect(r.queryByText(/%/)).toBeNull()
  })

  it('does not show a layer or "provisional" on a shaped milestone', () => {
    view([repo(A, [iss(1, 1)], [ms(1, 1, 1)])])
    const r = within(row(`${A}#m1`))
    expect(r.queryByTestId('shaping-layer')).toBeNull()
    expect(r.queryByText(/provisional/)).toBeNull()
  })

  it('sorts like sortMilestoneRows: fewest open first, empty last', () => {
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
    const btn = (key: string, name: 'Omhoog' | 'Omlaag') =>
      within(row(key)).getByRole('button', { name }) as HTMLButtonElement
    const order = () => screen.getAllByTestId(/^ms-/).map((e) => e.getAttribute('data-testid'))

    it('lists ranked milestones first by place, unranked below in computed order', () => {
      prio([ranked()])
      expect(order()).toEqual([`ms-${A}#m2`, `ms-${A}#m1`, `ms-${A}#m3`, `ms-${A}#m4`])
    })

    it('keeps the computed order in stand Berekend, without buttons or place', () => {
      prio([ranked()], { order: 'computed' })
      expect(order()).toEqual([`ms-${A}#m3`, `ms-${A}#m1`, `ms-${A}#m2`, `ms-${A}#m4`])
      expect(screen.queryAllByRole('button', { name: /Omhoog|Omlaag/ })).toHaveLength(0)
      expect(screen.queryAllByTestId('place')).toHaveLength(0)
    })

    it('shows the place in the repo next to the title, and no number without a rank', () => {
      prio([ranked()])
      expect(within(row(`${A}#m2`)).getByTestId('place').textContent).toBe('1')
      expect(within(row(`${A}#m1`)).getByTestId('place').textContent).toBe('2')
      expect(within(row(`${A}#m3`)).queryByTestId('place')).toBeNull()
    })

    it('disables up on place 1, down on the last ranked and down without a rank', () => {
      prio([ranked()])
      expect(btn(`${A}#m2`, 'Omhoog').disabled).toBe(true)
      expect(btn(`${A}#m2`, 'Omlaag').disabled).toBe(false)
      expect(btn(`${A}#m1`, 'Omhoog').disabled).toBe(false)
      expect(btn(`${A}#m1`, 'Omlaag').disabled).toBe(true)
      expect(btn(`${A}#m3`, 'Omhoog').disabled).toBe(false)
      expect(btn(`${A}#m3`, 'Omlaag').disabled).toBe(true)
    })

    it('calls onMove with the row and the direction of the clicked button', () => {
      const onMove = vi.fn<(row: MilestoneRow, dir: 'up' | 'down') => void>()
      prio([ranked()], { onMove })
      fireEvent.click(btn(`${A}#m1`, 'Omhoog'))
      fireEvent.click(btn(`${A}#m2`, 'Omlaag'))
      expect(onMove.mock.calls.map(([r, d]) => [r.repo, r.number, d])).toEqual([
        [A, 1, 'up'],
        [A, 2, 'down'],
      ])
    })

    it('disables every button of a repo with a running request, not those of another repo', () => {
      const other = repo(B, [], [ms(7, 1, 0, 'Priority: 1'), ms(8, 1, 0, 'Priority: 2')])
      prio([ranked(), other], { pendingRepos: new Set([A]) }, [A, B])
      for (const k of [`${A}#m1`, `${A}#m2`, `${A}#m3`]) {
        expect(btn(k, 'Omhoog').disabled).toBe(true)
        expect(btn(k, 'Omlaag').disabled).toBe(true)
      }
      expect(btn(`${B}#m8`, 'Omhoog').disabled).toBe(false)
      expect(btn(`${B}#m7`, 'Omlaag').disabled).toBe(false)
    })

    it('numbers places per repo when repos are mixed', () => {
      const other = repo(B, [], [ms(7, 1, 0, 'Priority: 5')])
      prio([ranked(), other], {}, [A, B])
      expect(within(row(`${B}#m7`)).getByTestId('place').textContent).toBe('1')
      expect(order()).toEqual([`ms-${B}#m7`, `ms-${A}#m2`, `ms-${A}#m1`, `ms-${A}#m3`, `ms-${A}#m4`])
    })
  })
})
