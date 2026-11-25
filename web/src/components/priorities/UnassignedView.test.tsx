import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { Issue, Repo } from '@/api/lighthouse'
import { buildGraph } from '@/lib/priorities/derive'
import { UnassignedView } from './UnassignedView'

afterEach(cleanup)

const A = 'a/x'
const B = 'b/y'
const iss = (
  number: number,
  by: Array<[string, number]> = [],
  milestone?: { number: number; state: string; title: string; url: string },
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
  ...(milestone ? { milestone } : {}),
})
const repo = (slug: string, issues: Issue[]): Repo => ({ slug, issues, milestones: [] })

function view(repos: Repo[], selected: string[] = [A]) {
  const graph = buildGraph(repos, [])
  return render(
    <UnassignedView graph={graph} repos={repos} selected={new Set(selected)} colors={new Map()} />,
  )
}
const rows = () => screen.queryAllByTestId('unassigned-row')

describe('UnassignedView', () => {
  it('shows number as forge link and title per open issue without milestone', () => {
    view([repo(A, [iss(5, []), iss(6, [], { number: 1, state: 'open', title: 'M1', url: 'u-m1' })])])
    expect(rows()).toHaveLength(1)
    const link = screen.getByRole('link', { name: '5' })
    expect(link.getAttribute('href')).toBe('https://forge/5')
    expect(screen.getByText('title-5')).toBeTruthy()
  })

  it('names the relation with the related number', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]]), iss(3, [[A, 44]]), iss(44)])])
    const byNr = (n: number) => rows().find((r) => within(r).queryByText(`title-${n}`))!
    expect(within(byNr(1)).getByText('blocks 2')).toBeTruthy()
    expect(within(byNr(2)).getByText('waits on 1')).toBeTruthy()
    expect(within(byNr(3)).getByText('waits on 44')).toBeTruthy()
    expect(within(byNr(44)).getByText('blocks 3')).toBeTruthy()
  })

  it('draws the relation pill blue, not yellow', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]])])])
    const pill = screen.getByText('waits on 1')
    expect(pill.getAttribute('style')).toContain('--lh-blue')
    expect(pill.getAttribute('style')).not.toContain('--lh-yellow')
  })

  it('lists issues with a relation first, then number descending', () => {
    view([repo(A, [iss(9), iss(8), iss(1), iss(2, [[A, 1]])])])
    const order = rows().map((r) => r.getAttribute('data-number'))
    expect(order).toEqual(['2', '1', '9', '8'])
  })

  it('marks an open issue in a closed milestone with the title and the word closed', () => {
    view([repo(A, [iss(7, [], { number: 3, state: 'closed', title: 'Old one', url: 'u-old' })])])
    const r = rows()[0]
    expect(within(r).getByText('Old one · closed')).toBeTruthy()
  })

  it('shows no milestone label for an issue without any milestone', () => {
    view([repo(A, [iss(7)])])
    expect(within(rows()[0]).queryByText(/closed/)).toBeNull()
  })

  it('hides unselected repos only', () => {
    view([repo(A, [iss(1)]), repo(B, [iss(2)])], [B])
    expect(rows()).toHaveLength(1)
    expect(screen.getByText('title-2')).toBeTruthy()
    expect(screen.queryByText('title-1')).toBeNull()
  })

  it('says one sentence and no list when there are none', () => {
    view([repo(A, [iss(1, [], { number: 1, state: 'open', title: 'M1', url: 'u-m1' })])])
    expect(screen.getByText('No open issues without a milestone.')).toBeTruthy()
    expect(screen.queryByRole('list')).toBeNull()
    expect(rows()).toHaveLength(0)
  })

  it('has a view root, no drag handles and no suggestions', () => {
    view([repo(A, [iss(1)])])
    const root = screen.getByTestId('view-unassigned')
    expect(root.querySelector('[draggable]')).toBeNull()
    expect(root.querySelector('button, select')).toBeNull()
  })
})
