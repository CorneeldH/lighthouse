import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchQuestions } from '@/lib/api'
import { newQueryClient, queryKeys } from '@/lib/queries'
import { Header } from './Header'

const clients: ReturnType<typeof newQueryClient>[] = []

function renderHeader() {
  const client = newQueryClient()
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <Header />
    </QueryClientProvider>,
  )
  return client
}

function json(body: string, status = 200): Response {
  return new Response(body, { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((c) => c.clear())
  vi.unstubAllGlobals()
})

describe('Header', () => {
  it('shows the version', async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(json('{"version":"1.2.3"}')))
    vi.stubGlobal('fetch', fetchMock)

    renderHeader()

    expect(await screen.findByText('1.2.3')).toBeTruthy()
    expect(fetchMock).toHaveBeenCalledWith('/api/health', expect.anything())
  })

  it('shows an alert when fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    renderHeader()

    expect((await screen.findByRole('alert')).textContent).toContain('API unreachable')
  })

  it('shows an alert on status 500 without "lighthouse undefined"', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('{"title":"Internal Server Error","status":500,"detail":"boom"}', {
          status: 500,
          headers: { 'Content-Type': 'application/problem+json' },
        }),
      ),
    )

    renderHeader()

    expect((await screen.findByRole('alert')).textContent).toContain('API unreachable')
    expect(screen.queryByText(/undefined/)).toBeNull()
  })

  it('shows Refreshing… and is disabled until the refetch settles', async () => {
    let release: () => void = () => {}
    let calls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        if (url !== '/api/health') return Promise.resolve(json('{"runs":[],"deferred":[]}'))
        calls++
        if (calls === 1) return Promise.resolve(json('{"version":"1.2.3"}'))
        return new Promise<Response>((resolve) => {
          release = () => resolve(json('{"version":"1.2.3"}'))
        })
      }),
    )
    renderHeader()
    await screen.findByText('1.2.3')

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    const busy = await screen.findByRole('button', { name: 'Refreshing…' })
    expect((busy as HTMLButtonElement).disabled).toBe(true)
    expect(busy.getAttribute('aria-busy')).toBe('true')
    release()
    const idle = await screen.findByRole('button', { name: 'Refresh' })
    expect((idle as HTMLButtonElement).disabled).toBe(false)
  })

  it('refetches every query in the client on Refresh', async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(url === '/api/health' ? json('{"version":"1.2.3"}') : json('[]')),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = renderHeader()
    await client.prefetchQuery({
      queryKey: queryKeys.questions,
      queryFn: ({ signal }) => fetchQuestions(signal),
    })
    await screen.findByText('1.2.3')
    const count = (path: string) => fetchMock.mock.calls.filter((c) => c[0] === path).length
    expect(count('/api/questions')).toBe(1)
    expect(count('/api/health')).toBe(1)

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    await vi.waitFor(() => expect(count('/api/questions')).toBe(2))
    await vi.waitFor(() => expect(count('/api/health')).toBe(2))
  })

  it('shows the age of the newest tick over runs and deferred starts', async () => {
    const ago = (ms: number) => new Date(Date.now() - ms - 30_000).toISOString()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          url === '/api/runs'
            ? json(
                JSON.stringify({
                  runs: [{ repo: 'o/a', issue: 3, at: ago(2 * 60_000) }],
                  deferred: [{ repo: 'o/a', issue: 4, at: ago(10 * 60_000) }],
                }),
              )
            : json('{"version":"1.2.3"}'),
        ),
      ),
    )

    renderHeader()

    expect(await screen.findByText('tick 2 min ago')).toBeTruthy()
  })

  it('shows no tick without runs or deferred starts', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(url === '/api/runs' ? json('{"runs":[],"deferred":[]}') : json('{"version":"1.2.3"}')),
      ),
    )

    renderHeader()

    await screen.findByText('1.2.3')
    await screen.findByText(/^updated /)
    expect(screen.queryByText(/tick/)).toBeNull()
  })
})
