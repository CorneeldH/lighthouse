import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { newQueryClient, queryKeys } from '@/lib/queries'
import { ControllerSection } from './ControllerSection'

const clients: ReturnType<typeof newQueryClient>[] = []

function renderSection() {
  const client = newQueryClient()
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <ControllerSection>
        <p>child</p>
      </ControllerSection>
    </QueryClientProvider>,
  )
  return client
}

function json(body: string, status = 200): Response {
  return new Response(body, { status, headers: { 'Content-Type': 'application/json' } })
}

function stubRuns(make: () => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation(() => Promise.resolve(make())),
  )
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((c) => c.clear())
  vi.unstubAllGlobals()
})

describe('ControllerSection', () => {
  it('is a region named Controller around its children', () => {
    stubRuns(() => json('{"runs":[],"deferred":[]}'))

    renderSection()

    const region = screen.getByRole('region', { name: 'Controller' })
    expect(within(region).getByText('child')).toBeTruthy()
    expect(within(region).getByRole('heading', { level: 2, name: 'Controller' })).toBeTruthy()
  })

  it('shows the age of the newest tick over runs and deferred starts', async () => {
    const ago = (ms: number) => new Date(Date.now() - ms - 30_000).toISOString()
    stubRuns(() =>
      json(
        JSON.stringify({
          runs: [{ repo: 'o/a', issue: 3, at: ago(2 * 60_000) }],
          deferred: [{ repo: 'o/a', issue: 4, at: ago(10 * 60_000) }],
        }),
      ),
    )

    renderSection()

    expect(await screen.findByText('tick 2 min ago')).toBeTruthy()
  })

  it('shows no tick without runs or deferred starts', async () => {
    stubRuns(() => json('{"runs":[],"deferred":[]}'))

    const client = renderSection()

    await waitFor(() => expect(client.getQueryData(queryKeys.runs)).toMatchObject({ runs: [], deferred: [] }))
    expect(screen.queryByText(/tick/)).toBeNull()
  })

  it('shows the heading without a tick when /api/runs fails', async () => {
    stubRuns(() => json('{"title":"boom"}', 500))

    const client = renderSection()

    await waitFor(() => expect(client.getQueryState(queryKeys.runs)?.status).toBe('error'))
    expect(screen.getByRole('heading', { level: 2, name: 'Controller' })).toBeTruthy()
    expect(screen.queryByText(/tick/)).toBeNull()
  })
})
