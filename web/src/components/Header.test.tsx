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

    expect(await screen.findByText('lighthouse 1.2.3')).toBeTruthy()
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
    expect(screen.queryByText(/lighthouse undefined/)).toBeNull()
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
    await screen.findByText('lighthouse 1.2.3')
    const count = (path: string) => fetchMock.mock.calls.filter((c) => c[0] === path).length
    expect(count('/api/questions')).toBe(1)
    expect(count('/api/health')).toBe(1)

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    await vi.waitFor(() => expect(count('/api/questions')).toBe(2))
    await vi.waitFor(() => expect(count('/api/health')).toBe(2))
  })
})
