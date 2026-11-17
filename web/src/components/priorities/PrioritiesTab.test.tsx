import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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
        { number: 1, title: 't1', url: 'u1', status: 'To do', blocked_by: null, milestone: M },
        { number: 2, title: 't2', url: 'u2', status: 'To do', blocked_by: [{ repo: 'a/x', issue: 1 }], milestone: M },
        { number: 3, title: 't3', url: 'u3', status: 'To do', blocked_by: null },
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

beforeEach(() => localStorage.clear())
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
    window.location.hash = '#prioritering'
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
})
