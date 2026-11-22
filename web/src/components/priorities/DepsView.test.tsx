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
    expect(within(node(`${A}#1`)).getByText('#1').className).toContain('font-semibold')
    expect(within(node(`${A}#1`)).getByText('title-1')).toBeTruthy()
    cleanup()
    view([repo(A, [iss(1), iss(2, [[A, 1]])]), repo('a/y', [])], [A, 'a/y'])
    expect(within(node(`${A}#1`)).getByText(`${A}#1`)).toBeTruthy()
  })

  it('marks a Controller issue without a run as active', () => {
    view([repo(A, [iss(1, [], { status: 'Controller' }), iss(2, [[A, 1]])])], [A])
    expect(node(`${A}#1`).getAttribute('data-state')).toBe('active')
    expect(within(node(`${A}#1`)).getByTestId('card-strip').className).toContain('lh-stripe')
    expect(within(node(`${A}#1`)).queryByText('picked up')).toBeNull()
  })

  it('marks an In progress issue as active', () => {
    view([repo(A, [iss(1, [], { status: 'In progress' }), iss(2, [[A, 1]])])], [A])
    expect(node(`${A}#1`).getAttribute('data-state')).toBe('active')
    expect(within(node(`${A}#1`)).getByTestId('card-strip').className).toContain('lh-stripe')
    expect(within(node(`${A}#1`)).queryByText('picked up')).toBeNull()
  })

  it('draws a ready card with a solid strip, and no dashed outline', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]])])], [A])
    expect(node(`${A}#1`).getAttribute('data-state')).toBe('ready')
    expect(within(node(`${A}#1`)).getByTestId('card-strip').className).not.toContain('lh-stripe')
    expect(node(`${A}#1`).getAttribute('data-needs-human')).toBe('false')
    expect(node(`${A}#1`).style.outline).not.toContain('dashed')
  })

  it('draws needs human as a dashed outline, also on an active card', () => {
    view(
      [
        repo(A, [
          iss(1, [], { label: 'needs_decision' }),
          iss(2, [], { label: 'needs_hands' }),
          iss(3, [], { status: 'In progress', open_question: true }),
          iss(4, [[A, 1], [A, 2], [A, 3]]),
        ]),
      ],
      [A],
    )
    for (const k of [1, 2]) {
      expect(node(`${A}#${k}`).getAttribute('data-needs-human')).toBe('true')
      expect(node(`${A}#${k}`).style.outline).toContain('dashed')
    }
    expect(node(`${A}#3`).getAttribute('data-state')).toBe('active')
    expect(node(`${A}#3`).getAttribute('data-needs-human')).toBe('true')
    expect(node(`${A}#3`).style.outline).toContain('dashed')
  })

  it('puts a cross at the number of a blocked card', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]])])], [A])
    expect(within(node(`${A}#2`)).getByTestId('card-cross')).toBeTruthy()
    expect(within(node(`${A}#1`)).queryByTestId('card-cross')).toBeNull()
  })

  it('shows the waiting pill only from two waiting issues', () => {
    view([repo(A, [iss(1), iss(2, [[A, 1]]), iss(3, [[A, 1]]), iss(4, [[A, 3]])])], [A])
    // #1 has 3 waiting, #3 has 1, #2 has 0.
    expect(within(node(`${A}#1`)).getByText('3')).toBeTruthy()
    expect(within(node(`${A}#3`)).queryByTestId('waiting-pill')).toBeNull()
    expect(within(node(`${A}#2`)).queryByTestId('waiting-pill')).toBeNull()
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
    expect(ext.style.outline).not.toContain('dashed')
    expect(ext.getAttribute('data-needs-human')).toBe('false')
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
    expect(card.textContent).toContain('#5')
  })

  it('shows a loose issue as a full card with number, title and state', () => {
    view(CHAIN, [A])
    const card = screen.getByTestId(`loose-${A}#5`)
    expect(within(card).getByText('#5')).toBeTruthy()
    expect(within(card).getByText('title-5')).toBeTruthy()
    expect(card.getAttribute('data-state')).toBe('ready')
    expect(card.style.width).toBe('210px')
    expect(card.style.height).toBe('38px')
  })

  it('marks a loose In progress issue as active', () => {
    view([repo(A, [iss(1, [], { status: 'In progress' })])], [A])
    const card = screen.getByTestId(`loose-${A}#1`)
    expect(card.getAttribute('data-state')).toBe('active')
    expect(within(card).getByTestId('card-strip').className).toContain('lh-stripe')
  })

  it('marks a loose Controller issue without a run as active', () => {
    view([repo(A, [iss(1, [], { status: 'Controller' })])], [A])
    const card = screen.getByTestId(`loose-${A}#1`)
    expect(card.getAttribute('data-state')).toBe('active')
    expect(within(card).getByTestId('card-strip').className).toContain('lh-stripe')
  })

  it('draws a cross on a loose card with a blocked_external label', () => {
    view([repo(A, [iss(1, [], { label: 'blocked_external' })])], [A])
    const card = screen.getByTestId(`loose-${A}#1`)
    expect(card.getAttribute('data-state')).toBe('blocked')
    expect(within(card).getByTestId('card-cross')).toBeTruthy()
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
