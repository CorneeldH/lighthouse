import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { Issue, Repo, Run } from '@/api/lighthouse'
import { buildGraph } from '@/lib/priorities/derive'
import { assignColors } from '@/lib/priorities/palette'
import { DepsView } from './DepsView'

afterEach(cleanup)

const iss = (number: number, by: Array<[string, number]> = [], extra: Partial<Issue> = {}): Issue => ({
  number,
  title: `title-${number}`,
  url: `https://forge/${number}`,
  status: 'To do',
  label: '',
  open_question: false,
  needs_label: false,
  prio_weight: 0,
  factor: 0,
  blocked_by: by.length ? by.map(([repo, issue]) => ({ repo, issue })) : null,
  ...extra,
})
const repo = (slug: string, issues: Issue[]): Repo => ({ slug, issues, milestones: [] })

function view(repos: Repo[], selected: string[], runs: Run[] = []) {
  const graph = buildGraph(repos, runs)
  const nodes = [...graph.nodes.values()].filter((n) => selected.includes(n.repo))
  const colors = assignColors(
    nodes.flatMap((n) => (n.milestone ? [{ repo: n.repo, number: n.milestone.number }] : [])),
  )
  return render(<DepsView graph={graph} repos={repos} selected={new Set(selected)} colors={colors} />)
}
const node = (key: string) => screen.getByTestId(`node-${key}`)

const A = 'a/x'
const CHAIN = [repo(A, [iss(1), iss(2, [[A, 1]]), iss(3, [[A, 1]]), iss(4, [[A, 2], [A, 3]]), iss(5)])]

describe('DepsView', () => {
  it('names the columns after how many steps a node waits', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]]), iss(3, [[A, 2]]), iss(4, [[A, 1]])])], [A])
    expect(screen.getByText('Can start now')).toBeTruthy()
    expect(screen.getByText('Waits on 1 step')).toBeTruthy()
    expect(screen.getByText('Waits on 2 steps')).toBeTruthy()
  })

  it('shows the number bold with the title, and prefixes the repo over more than one repo', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]])]), repo('a/y', [])], [A])
    expect(within(node(`${A}#1`)).getByText('1').className).toContain('font-semibold')
    expect(within(node(`${A}#1`)).getByText('title-1')).toBeTruthy()
    cleanup()
    view([repo(A, [iss(1), iss(2, [[A, 1]])]), repo('a/y', [])], [A, 'a/y'])
    expect(within(node(`${A}#1`)).getByText(`${A} 1`)).toBeTruthy()
    expect(within(node(`${A}#1`)).queryByText(/#/)).toBeNull()
  })

  it('marks a Controller issue without a run as active', () => {
    view([repo(A, [iss(1, [], { status: 'Controller' }), iss(2, [[A, 1]])])], [A])
    expect(node(`${A}#1`).getAttribute('data-state')).toBe('active')
    expect(within(node(`${A}#1`)).getByTestId('card-status').getAttribute('data-state')).toBe('active')
    expect(within(node(`${A}#1`)).getByTestId('card-strip').className).not.toContain('lh-stripe')
    expect(within(node(`${A}#1`)).queryByText('picked up')).toBeNull()
  })

  it('marks an In progress issue as active', () => {
    view([repo(A, [iss(1, [], { status: 'In progress' }), iss(2, [[A, 1]])])], [A])
    expect(node(`${A}#1`).getAttribute('data-state')).toBe('active')
    expect(within(node(`${A}#1`)).getByTestId('card-status').getAttribute('data-state')).toBe('active')
    expect(within(node(`${A}#1`)).getByTestId('card-strip').className).not.toContain('lh-stripe')
  })

  it('draws every state as a 14px status block before the number, with a solid strip', () => {
    view(
      [
        repo(A, [
          iss(1),
          iss(2, [[A, 1]]),
          iss(3, [], { label: 'needs_shaping' }),
          iss(4, [], { label: 'needs_decision' }),
          iss(5, [], { status: 'In progress' }),
          iss(6, [[A, 3], [A, 4], [A, 5]]),
        ]),
      ],
      [A],
    )
    const want: Record<number, string> = { 1: 'ready', 2: 'blocked', 3: 'needs_shaping', 4: 'needs_human', 5: 'active' }
    for (const [k, st] of Object.entries(want)) {
      const card = node(`${A}#${k}`)
      expect(card.getAttribute('data-state')).toBe(st)
      const b = within(card).getByTestId('card-status')
      expect(b.getAttribute('data-state')).toBe(st)
      expect(b.className).toContain('size-3.5')
      expect(within(card).getByTestId('card-strip').className).not.toContain('lh-stripe')
      expect(card.style.outline).toBe('')
      expect(card.hasAttribute('data-needs-human')).toBe(false)
      expect(within(card).queryByTestId('card-cross')).toBeNull()
    }
  })

  it('underlines the number for needs shaping and needs human only, without a hash', () => {
    view(
      [
        repo(A, [
          iss(1),
          iss(3, [], { label: 'needs_shaping' }),
          iss(4, [], { label: 'needs_hands' }),
          iss(5, [], { status: 'In progress', open_question: true }),
          iss(6, [[A, 3], [A, 4], [A, 5], [A, 1]]),
        ]),
      ],
      [A],
    )
    for (const k of [3, 4, 5]) {
      const n = within(node(`${A}#${k}`)).getByText(String(k))
      expect(n.getAttribute('data-underlined')).toBe('true')
      expect(n.className).toContain('underline')
    }
    expect(node(`${A}#5`).getAttribute('data-state')).toBe('needs_human')
    for (const k of [1, 6]) {
      const n = within(node(`${A}#${k}`)).getByText(String(k))
      expect(n.getAttribute('data-underlined')).toBeNull()
      expect(n.className).not.toContain('underline')
    }
  })

  it('shows the waiting pill only from two waiting issues', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]]), iss(3, [[A, 1]]), iss(4, [[A, 3]])])], [A])
    // #1 has 3 waiting, #3 has 1, #2 has 0.
    expect(within(node(`${A}#1`)).getByText('3')).toBeTruthy()
    expect(within(node(`${A}#3`)).queryByTestId('waiting-pill')).toBeNull()
    expect(within(node(`${A}#2`)).queryByTestId('waiting-pill')).toBeNull()
  })

  it('the pill shows the waiting count; its title carries the weights', () => {
    view(
      [repo(A, [iss(1, [], { factor: 3, prio_weight: 10 }), iss(2, [[A, 1]]), iss(3, [[A, 1]]), iss(4, [[A, 1]])])],
      [A],
    )
    const pill = within(node(`${A}#1`)).getByTestId('waiting-pill')
    expect(pill.textContent).toBe('3')
    expect(pill.getAttribute('title')).toBe('3 wait · weight 3 · factor 3 · prio-weight 10')
  })

  it('keeps a blocker in a filtered-out repo as a solid grey node, and the column of its dependent', () => {
    const other = repo('a/y', [iss(9)])
    view([repo(A, [iss(2, [['a/y', 9]]), iss(3, [[A, 2]])]), other], [A])
    const blocker = node('a/y#9')
    expect(blocker.getAttribute('data-external')).toBe('true')
    expect(blocker.className).toContain('border-lh-grey')
    expect(blocker.className).not.toContain('border-dashed')
    // the dependent waits 1 step in the full computation, so the heading exists and "can start" holds #9 only
    expect(screen.getByText('Waits on 1 step')).toBeTruthy()
    expect(screen.getByText('Waits on 2 steps')).toBeTruthy()
    expect(node(`${A}#2`).getAttribute('data-depth')).toBe('1')
    expect(node('a/y#9').getAttribute('data-depth')).toBe('0')
  })

  it('gives an external card a solid grey border and never a needs-human outline', () => {
    view([repo(A, [iss(2, [['a/y', 9]])]), repo('a/y', [iss(9, [], { label: 'needs_decision' })])], [A])
    const ext = node('a/y#9')
    expect(ext.className).toContain('border-lh-grey')
    expect(ext.className).not.toContain('border-dashed')
    expect(ext.getAttribute('data-external')).toBe('true')
    expect(within(ext).queryByTestId('card-status')).toBeNull()
    expect(ext.querySelector('[data-underlined]')).toBeNull()
    expect(within(ext).getByText('a/y 9').className).not.toContain('underline')
    expect(ext.style.outline).toBe('')
    expect(ext.hasAttribute('data-needs-human')).toBe(false)
  })

  it('does not show a dependent that lives in a filtered-out repo', () => {
    view([repo(A, [iss(1)]), repo('a/y', [iss(5, [[A, 1]])])], [A])
    expect(screen.queryByTestId('node-a/y#5')).toBeNull()
  })

  it('dims every node outside the chain on hover, and undims on leave', () => {
    view(CHAIN, [A])
    // The chain of #2 is 1 -> 2 -> 4; #3 is a sibling, on neither side of it.
    fireEvent.mouseEnter(node(`${A}#2`))
    expect(node(`${A}#3`).getAttribute('data-dimmed')).toBe('true')
    for (const k of [1, 2, 4]) expect(node(`${A}#${k}`).getAttribute('data-dimmed')).toBe('false')
    fireEvent.mouseLeave(node(`${A}#2`))
    expect(node(`${A}#3`).getAttribute('data-dimmed')).toBe('false')
  })

  it('opens the issue through a link to node.url', () => {
    view(CHAIN, [A])
    expect(node(`${A}#1`).tagName).toBe('A')
    expect(node(`${A}#1`).getAttribute('href')).toBe('https://forge/1')
  })

  it('on touch the first tap lights the chain, the second opens the issue', () => {
    view(CHAIN, [A])
    const a = node(`${A}#2`)
    fireEvent.pointerDown(a, { pointerType: 'touch' })
    expect(fireEvent.click(a)).toBe(false) // default prevented: no navigation
    expect(node(`${A}#3`).getAttribute('data-dimmed')).toBe('true')
    fireEvent.pointerDown(a, { pointerType: 'touch' })
    expect(fireEvent.click(a)).toBe(true)
  })

  it('lists the issues without relations under the graph as full cards', () => {
    view(CHAIN, [A])
    expect(screen.getByText('No open dependencies (1)')).toBeTruthy()
    const card = screen.getByTestId(`loose-${A}#5`)
    expect(within(card).getByText('title-5').getAttribute('title')).toBe('title-5')
    expect(card.textContent).toContain('5title-5')
  })

  it('shows a loose issue as a full card with number, title and state', () => {
    view(CHAIN, [A])
    const card = screen.getByTestId(`loose-${A}#5`)
    expect(within(card).getByText('5')).toBeTruthy()
    expect(within(card).getByText('title-5')).toBeTruthy()
    expect(card.getAttribute('data-state')).toBe('ready')
    expect(within(card).getByTestId('card-strip').style.background).not.toBe('')
    expect(card.style.width).toBe('210px')
    expect(card.style.height).toBe('38px')
  })

  it('marks a loose In progress issue as active', () => {
    view([repo(A, [iss(1, [], { status: 'In progress' })])], [A])
    const card = screen.getByTestId(`loose-${A}#1`)
    expect(card.getAttribute('data-state')).toBe('active')
    expect(within(card).getByTestId('card-status').getAttribute('data-state')).toBe('active')
    expect(within(card).getByTestId('card-strip').className).not.toContain('lh-stripe')
  })

  it('marks a loose Controller issue without a run as active', () => {
    view([repo(A, [iss(1, [], { status: 'Controller' })])], [A])
    const card = screen.getByTestId(`loose-${A}#1`)
    expect(card.getAttribute('data-state')).toBe('active')
    expect(within(card).getByTestId('card-strip').className).not.toContain('lh-stripe')
  })

  it('draws a blocked status block on a loose card with a blocked_external label', () => {
    view([repo(A, [iss(1, [], { label: 'blocked_external' })])], [A])
    const card = screen.getByTestId(`loose-${A}#1`)
    expect(card.getAttribute('data-state')).toBe('blocked')
    expect(within(card).getByTestId('card-status').getAttribute('data-state')).toBe('blocked')
  })

  it('positions a graph card absolutely through its inline style', () => {
    view(CHAIN, [A])
    const card = node(`${A}#1`)
    expect(card.style.position).toBe('absolute')
    expect(card.style.left).not.toBe('')
    expect(card.style.top).not.toBe('')
  })

  it('leaves a loose card in the grid flow, not absolute', () => {
    view(CHAIN, [A])
    const card = screen.getByTestId(`loose-${A}#5`)
    expect(card.style.position).not.toBe('absolute')
    expect(card.className.split(/\s+/)).not.toContain('absolute')
  })

  it('says so in one sentence and draws no svg without any relation', () => {
    const { container } = view([repo(A, [iss(1), iss(2)])], [A])
    expect(screen.getByText('No open dependencies between these issues.')).toBeTruthy()
    expect(container.querySelector('svg')).toBeNull()
    expect(screen.getByText('No open dependencies (2)')).toBeTruthy()
  })

  it('scrolls the graph horizontally in its own container', () => {
    view(CHAIN, [A])
    expect(screen.getByTestId('graph-scroll').className).toContain('overflow-x-auto')
  })

  it('marks the members of a cycle', () => {
    view([repo(A, [iss(1, [[A, 2]]), iss(2, [[A, 1]]), iss(3)])], [A])
    expect(node(`${A}#1`).getAttribute('data-cycle')).toBe('true')
    expect(within(node(`${A}#1`)).getByText('cycle')).toBeTruthy()
  })

  it('draws one arrow with an arrowhead per visible relation', () => {
    const { container } = view(CHAIN, [A])
    expect(container.querySelectorAll('path[data-from]')).toHaveLength(4)
    expect(container.querySelector('path[data-from]')?.getAttribute('marker-end')).toContain('url(#')
  })

  it('lets a real mouse take over from a touch pin', () => {
    view(CHAIN, [A])
    const a = node(`${A}#2`)
    fireEvent.pointerDown(a, { pointerType: 'touch' })
    fireEvent.click(a)
    expect(node(`${A}#3`).getAttribute('data-dimmed')).toBe('true')
    const other = node(`${A}#3`)
    fireEvent.pointerEnter(other, { pointerType: 'mouse' })
    fireEvent.mouseEnter(other)
    // hover now decides: the chain of #3 is 1 -> 3 -> 4, so #2 is dimmed and #3 is not
    expect(other.getAttribute('data-dimmed')).toBe('false')
    expect(a.getAttribute('data-dimmed')).toBe('true')
  })

  it('draws no arrow for a shown node whose only relation is a dependent in a filtered-out repo', () => {
    const { container } = view([repo(A, [iss(1)]), repo('a/y', [iss(5, [[A, 1]])])], [A])
    // #1 has a relation (so it is not "loose") but the dependent is hidden: node, no arrow.
    expect(screen.getByTestId(`node-${A}#1`)).toBeTruthy()
    expect(container.querySelectorAll('path[data-from]')).toHaveLength(0)
    expect(screen.getByText('No open dependencies (0)')).toBeTruthy()
  })
})
