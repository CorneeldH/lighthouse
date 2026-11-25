import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '@/App'
import { newQueryClient } from '@/lib/queries'
import { PrioritiesTab } from './PrioritiesTab'

const json = (body: unknown, status = 200) =>
  Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))

const M = { number: 1, state: 'open', title: 'Alpha' }
const PRIORITIES = {
  repos: [
    {
      slug: 'a/x',
      issues: [
        { number: 1, title: 't1', url: 'u1', status: 'To do', labels: [], open_question: false, blocked_by: null, milestone: M },
        { number: 2, title: 't2', url: 'u2', status: 'To do', labels: [], open_question: false, blocked_by: [{ repo: 'a/x', issue: 1 }], milestone: M },
        { number: 3, title: 't3', url: 'u3', status: 'To do', labels: [], open_question: false, blocked_by: null },
      ],
      milestones: [{ number: 1, title: 'Alpha', description: '', done: 0, open: 2 }],
    },
    { slug: 'a/y', issues: [], milestones: [] },
  ],
  runs: [],
}

function stub(priorities: () => Promise<Response>) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (url === '/api/priorities') return priorities()
      if (url === '/api/questions') return json({ questions: [] })
      return new Promise<Response>(() => {})
    }),
  )
}

function renderTab() {
  const client = newQueryClient()
  render(
    <QueryClientProvider client={client}>
      <PrioritiesTab />
    </QueryClientProvider>,
  )
  return client
}

beforeEach(() => {
  localStorage.clear()
  // jsdom has no ResizeObserver; the milestone bar measures itself with one.
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  window.location.hash = ''
})

describe('PrioritiesTab', () => {
  it('shows Loading… while the request is pending', () => {
    stub(() => new Promise<Response>(() => {}))
    renderTab()
    expect(screen.getByText('Loading…')).toBeTruthy()
  })

  it('all repo chips start on', async () => {
    stub(() => json(PRIORITIES))
    renderTab()
    const chip = await screen.findByRole('button', { name: 'a/x' })
    expect(chip.getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByRole('button', { name: 'a/y' }).getAttribute('aria-pressed')).toBe('true')
  })

  it('shows the counts in the switcher', async () => {
    stub(() => json(PRIORITIES))
    renderTab()
    await screen.findByRole('button', { name: 'a/x' })
    expect(screen.getByRole('button', { name: /Dependencies/ }).textContent).toContain('3')
    expect(screen.getByRole('button', { name: /^Milestones/ }).textContent).toContain('1')
    expect(screen.getByRole('button', { name: /Without milestone/ }).textContent).toContain('1')
  })

  it('a chip toggle filters the counts and survives a remount through the prefs', async () => {
    stub(() => json(PRIORITIES))
    renderTab()
    fireEvent.click(await screen.findByRole('button', { name: 'a/x' }))
    expect(screen.getByRole('button', { name: 'a/x' }).getAttribute('aria-pressed')).toBe('false')
    expect(screen.getByRole('button', { name: /Dependencies/ }).textContent).toContain('0')
    cleanup()
    renderTab()
    expect((await screen.findByRole('button', { name: 'a/x' })).getAttribute('aria-pressed')).toBe('false')
  })

  it('the switcher swaps the view', async () => {
    stub(() => json(PRIORITIES))
    renderTab()
    await screen.findByTestId('view-deps')
    fireEvent.click(screen.getByRole('button', { name: /^Milestones/ }))
    expect(screen.getByTestId('view-milestones')).toBeTruthy()
    expect(screen.queryByTestId('view-deps')).toBeNull()
  })

  it('works with a throwing localStorage', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    stub(() => json(PRIORITIES))
    renderTab()
    await screen.findByTestId('view-deps')
    fireEvent.click(screen.getByRole('button', { name: /^Milestones/ }))
    expect(screen.getByTestId('view-milestones')).toBeTruthy()
  })

  it('a 502 shows the forge error in the card while the Controller tab still renders', async () => {
    stub(() => json({ error: 'boom' }, 502))
    window.location.hash = '#priorities'
    const client = newQueryClient()
    render(
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>,
    )
    const alert = await screen.findByText(/Forge unreachable/)
    expect(alert.closest('[id="priorities"]')).not.toBeNull()
    fireEvent.click(screen.getByRole('tab', { name: 'Controller' }))
    await waitFor(() => expect(document.getElementById('questions')).not.toBeNull())
    client.clear()
  })

  describe('legend and colours', () => {
    const iss = (n: number, ms: { number: number; title: string } | null) => ({
      number: n, title: `t${n}`, url: `u${n}`, status: 'To do', labels: [], open_question: false, blocked_by: null,
      ...(ms ? { milestone: { ...ms, state: 'open' } } : {}),
    })
    const ms = (number: number, title: string) => ({ number, title, description: '', done: 0, open: 1 })
    const TWO = {
      repos: [
        { slug: 'a/x', issues: [iss(1, { number: 1, title: 'Alpha' })], milestones: [ms(1, 'Alpha'), ms(2, 'Empty')] },
        { slug: 'a/y', issues: [iss(1, { number: 5, title: 'Beta' })], milestones: [ms(5, 'Beta')] },
      ],
      runs: [],
    }
    const STATE_LABELS = ['needs mind', 'to do', 'active', 'needs hands', 'done']
    const milestones = () => within(screen.getByRole('list', { name: 'Milestones', hidden: false }))
    const statusList = () => screen.getByRole('list', { name: 'Status' })
    const legend = milestones
    const swatches = () =>
      Array.from(screen.getByRole('list', { name: 'Milestones' }).querySelectorAll('li'))
        .map((li) => [li.textContent, (li.querySelector('i') as HTMLElement).style.background])

    it('lists only milestones with a node in the deps view, with repo prefix for >1 repo', async () => {
      stub(() => json(TWO))
      renderTab()
      await screen.findByRole('button', { name: 'a/x' })
      expect(legend().getByText('a/x · Alpha')).toBeTruthy()
      expect(legend().getByText('a/y · Beta')).toBeTruthy()
      expect(legend().queryByText(/Empty/)).toBeNull()
    })

    it('dropping a repo drops its milestones, the prefix goes and the colours restart', async () => {
      stub(() => json(TWO))
      renderTab()
      const before = await screen.findByRole('button', { name: 'a/x' })
      expect(swatches()[1][1]).toBe('var(--lh-purple)')
      fireEvent.click(before)
      expect(legend().queryByText(/Alpha/)).toBeNull()
      expect(legend().getByText('Beta')).toBeTruthy()
      expect(swatches()[0]).toEqual(['Beta', 'var(--lh-blue)'])
    })

    it('the milestones view shows the rows, empty milestones included', async () => {
      stub(() => json(TWO))
      renderTab()
      fireEvent.click(await screen.findByRole('button', { name: /^Milestones/ }))
      expect(swatches().map((x) => x[0])).toEqual(['a/x · Alpha', 'a/x · Empty', 'a/y · Beta'])
    })

    it('has a Milestones list and a Status list with headings on each tab', async () => {
      stub(() => json(TWO))
      renderTab()
      await screen.findByRole('button', { name: 'a/x' })
      for (const name of [/^Dependencies/, /^Milestones/, /Without milestone/]) {
        fireEvent.click(screen.getByRole('button', { name }))
        expect(screen.getByRole('list', { name: 'Milestones' })).toBeTruthy()
        expect(statusList()).toBeTruthy()
        expect(screen.getAllByText('Milestones', { selector: 'span,h3,p,div' }).length).toBeGreaterThan(0)
        expect(screen.getAllByText('Status').length).toBeGreaterThan(0)
        expect(screen.queryByRole('list', { name: 'Legend' })).toBeNull()
      }
    })

    it('the Status list has the five states in order, no shaping item and no picked-up pill', async () => {
      stub(() => json(TWO))
      renderTab()
      await screen.findByRole('button', { name: 'a/x' })
      const items = Array.from(statusList().querySelectorAll('li'))
      expect(items.map((li) => li.textContent)).toEqual(STATE_LABELS)
      const states = screen.getAllByTestId('legend-block')
      expect(states.map((b) => b.getAttribute('data-state'))).toEqual([
        'needs_mind',
        'ready',
        'active',
        'needs_hands',
        'done',
      ])
      expect(screen.queryByTestId('legend-shaping')).toBeNull()
      expect(within(statusList()).queryByText('picked up')).toBeNull()
      expect(within(statusList()).queryByText('needs a human')).toBeNull()
    })

    it('underlines only the labels needs mind and needs hands', async () => {
      stub(() => json(TWO))
      renderTab()
      await screen.findByRole('button', { name: 'a/x' })
      const underlined = Array.from(statusList().querySelectorAll('li'))
        .filter((li) => li.querySelector('[data-underlined="true"]') !== null)
        .map((li) => li.textContent)
      expect(underlined).toEqual(['needs mind', 'needs hands'])
    })

    it('adds a grey Without milestone item once issues without a milestone are in view', async () => {
      stub(() => json(PRIORITIES))
      renderTab()
      await screen.findByRole('button', { name: 'a/x' })
      expect(swatches().at(-1)).toEqual(['Without milestone', 'var(--lh-grey)'])
      fireEvent.click(screen.getByRole('button', { name: /^Milestones/ }))
      expect(milestones().queryByText('Without milestone')).toBeNull()
    })

    it('a shaping milestone keeps its dotted bar border and gets no legend item', async () => {
      const shaping = {
        repos: [{ slug: 'a/x', issues: [iss(1, { number: 1, title: 'Alpha' })], milestones: [{ ...ms(1, 'Alpha'), description: 'Status: shaping' }] }],
        runs: [],
      }
      stub(() => json(shaping))
      renderTab()
      fireEvent.click(await screen.findByRole('button', { name: /^Milestones/ }))
      expect(screen.getByTestId('bar').getAttribute('data-shaping')).toBe('true')
      expect(screen.queryByTestId('legend-shaping')).toBeNull()
      expect(screen.getByRole('list', { name: 'Milestones' }).querySelectorAll('li')).toHaveLength(1)
    })

    it('the without-milestone view has no milestone colours when no closed milestone is named', async () => {
      stub(() => json(TWO))
      renderTab()
      fireEvent.click(await screen.findByRole('button', { name: /Without milestone/ }))
      expect(screen.getByRole('list', { name: 'Milestones' }).querySelectorAll('li')).toHaveLength(0)
      expect(Array.from(statusList().querySelectorAll('li')).map((li) => li.textContent)).toEqual(STATE_LABELS)
    })

    it('a stored selection of only stale slugs counts as all on', async () => {
      localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'deps', repos: ['gone/z'] }))
      stub(() => json(TWO))
      renderTab()
      expect((await screen.findByRole('button', { name: 'a/x' })).getAttribute('aria-pressed')).toBe('true')
    })
  })

  describe('deps sort switch', () => {
    const I = (number: number, by: number[], prio_weight = 0) => ({
      number, title: `t${number}`, url: `u${number}`, status: 'To do', labels: [], open_question: false,
      prio_weight, factor: 0, blocked_by: by.length ? by.map((issue) => ({ repo: 'a/x', issue })) : null,
    })
    // Column 0: 1, 2, 3. Weights: 3 blocks two issues (2), 1 and 2 one each. Prio-weights run the other way round.
    const SORT = {
      repos: [
        {
          slug: 'a/x',
          issues: [I(1, [], 5), I(2, [], 1), I(3, [], 2), I(4, [3]), I(5, [3]), I(6, [1]), I(7, [2])],
          milestones: [],
        },
      ],
      runs: [],
    }
    const column = () =>
      [1, 2, 3]
        .map((n) => ({ n, top: Number.parseFloat(screen.getByTestId(`node-a/x#${n}`).style.top) }))
        .sort((a, b) => a.top - b.top)
        .map((x) => x.n)
    const sortGroup = () => within(screen.getByRole('group', { name: 'Sort' }))

    it('shows Prio-weight, Weight, Fewest crossings in Dependencies with Prio-weight pressed', async () => {
      stub(() => json(SORT))
      renderTab()
      await screen.findByTestId('view-deps')
      const names = sortGroup().getAllByRole('button').map((b) => b.textContent)
      expect(names).toEqual(['Prio-weight', 'Weight', 'Fewest crossings'])
      expect(sortGroup().getByRole('button', { name: 'Prio-weight' }).getAttribute('aria-pressed')).toBe('true')
      expect(sortGroup().getByRole('button', { name: 'Weight' }).getAttribute('aria-pressed')).toBe('false')
      expect(column()).toEqual([1, 3, 2])
    })

    it('clicking Weight stores depsOrder weight and reorders the column', async () => {
      stub(() => json(SORT))
      renderTab()
      await screen.findByTestId('view-deps')
      fireEvent.click(sortGroup().getByRole('button', { name: 'Weight' }))
      expect(sortGroup().getByRole('button', { name: 'Weight' }).getAttribute('aria-pressed')).toBe('true')
      expect(JSON.parse(localStorage.getItem('lighthouse.priorities') as string).depsOrder).toBe('weight')
      expect(column()[0]).toBe(3)
      expect(column()).not.toEqual([1, 3, 2])
    })

    it('works without localStorage', async () => {
      vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new Error('denied')
      })
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('denied')
      })
      stub(() => json(SORT))
      renderTab()
      await screen.findByTestId('view-deps')
      expect(sortGroup().getByRole('button', { name: 'Prio-weight' }).getAttribute('aria-pressed')).toBe('true')
      fireEvent.click(sortGroup().getByRole('button', { name: 'Weight' }))
      expect(column()[0]).toBe(3)
    })

    it('is absent in Milestones and Without milestone', async () => {
      stub(() => json(SORT))
      renderTab()
      await screen.findByTestId('view-deps')
      fireEvent.click(screen.getByRole('button', { name: /^Milestones/ }))
      expect(screen.queryByRole('group', { name: 'Sort' })).toBeNull()
      fireEvent.click(screen.getByRole('button', { name: /Without milestone/ }))
      expect(screen.queryByRole('group', { name: 'Sort' })).toBeNull()
    })
  })

  describe('priority switch', () => {
    // Computed order: Beta (0 open) before Alpha (2 open); the ranks put Alpha first.
    const RANKED = {
      repos: [
        {
          slug: 'a/x',
          issues: PRIORITIES.repos[0].issues,
          milestones: [
            { number: 1, title: 'Alpha', description: 'Priority: 1', done: 0, open: 2 },
            { number: 2, title: 'Beta', description: 'Priority: 2', done: 1, open: 0 },
          ],
        },
      ],
      runs: [],
    }
    const order = () => screen.getAllByTestId(/^ms-/).map((e) => e.getAttribute('data-testid'))

    it('shows the switch only in the milestones view, % done by default', async () => {
      stub(() => json(RANKED))
      renderTab()
      await screen.findByTestId('view-deps')
      expect(screen.queryByRole('button', { name: 'Priority' })).toBeNull()
      fireEvent.click(screen.getByRole('button', { name: /^Milestones/ }))
      const computed = screen.getByRole('button', { name: '% done' })
      expect(computed.textContent).toBe('%')
      expect(computed.getAttribute('aria-pressed')).toBe('true')
      expect(screen.getByRole('button', { name: 'Priority' }).getAttribute('aria-pressed')).toBe('false')
      expect(order()).toEqual(['ms-a/x#m2', 'ms-a/x#m1'])
    })

    it('Priority reorders the list and survives a remount through the prefs', async () => {
      stub(() => json(RANKED))
      renderTab()
      await screen.findByTestId('view-deps')
      fireEvent.click(screen.getByRole('button', { name: /^Milestones/ }))
      fireEvent.click(screen.getByRole('button', { name: 'Priority' }))
      expect(screen.getByRole('button', { name: 'Priority' }).getAttribute('aria-pressed')).toBe('true')
      expect(order()).toEqual(['ms-a/x#m1', 'ms-a/x#m2'])
      expect(JSON.parse(localStorage.getItem('lighthouse.priorities') as string).msOrder).toBe('priority')
      cleanup()
      renderTab()
      expect((await screen.findByRole('button', { name: 'Priority' })).getAttribute('aria-pressed')).toBe('true')
      expect(order()).toEqual(['ms-a/x#m1', 'ms-a/x#m2'])
    })

    it('a click posts the planned swap, and a 409 shows the conflict text', async () => {
      localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'milestones', repos: null, msOrder: 'priority' }))
      const posts: Array<{ body: unknown; resolve: (r: Response) => void }> = []
      vi.stubGlobal(
        'fetch',
        vi.fn().mockImplementation((url: string, init?: RequestInit) => {
          if (url === '/api/priorities') return json(RANKED)
          if (url === '/api/priorities/milestone-ranks' && init?.method === 'POST') {
            return new Promise<Response>((resolve) => posts.push({ body: JSON.parse(init.body as string), resolve }))
          }
          return new Promise<Response>(() => {})
        }),
      )
      const client = renderTab()
      const beta = await screen.findByTestId('ms-a/x#m2')
      fireEvent.click(within(beta).getByRole('button', { name: /up/i }))
      await waitFor(() => expect(posts).toHaveLength(1))
      expect(posts[0].body).toEqual({
        repo: 'a/x',
        changes: [
          { number: 2, from: 2, to: 1 },
          { number: 1, from: 1, to: 2 },
        ],
      })
      // Optimistic: Beta is on top while the request runs, and its buttons are off.
      expect(order()).toEqual(['ms-a/x#m2', 'ms-a/x#m1'])
      expect((within(screen.getByTestId('ms-a/x#m2')).getByRole('button', { name: /down/i }) as HTMLButtonElement).disabled).toBe(true)
      posts[0].resolve(await json({ detail: 'rank changed' }, 409))
      const alert = await screen.findByRole('alert')
      expect(alert.textContent).toBe('The order was just changed elsewhere; the list has been refreshed.')
      await waitFor(() => expect(order()).toEqual(['ms-a/x#m1', 'ms-a/x#m2']))
      client.clear()
    })
  })
})
