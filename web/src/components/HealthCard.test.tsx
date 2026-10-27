import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { HealthCard } from './HealthCard'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('HealthCard', () => {
  it('shows the version', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{"version":"1.2.3"}', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    render(<HealthCard />)

    expect(await screen.findByText('lighthouse 1.2.3')).toBeTruthy()
    expect(fetchMock).toHaveBeenCalledWith('/api/health', expect.anything())
  })

  it('shows an error when fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    render(<HealthCard />)

    expect((await screen.findByRole('alert')).textContent).toContain(
      'API unreachable: Failed to fetch',
    )
  })

  it('shows an error on a non-200 status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('{"title":"Internal Server Error","status":500,"detail":"boom"}', {
          status: 500,
          headers: { 'Content-Type': 'application/problem+json' },
        }),
      ),
    )

    render(<HealthCard />)

    expect((await screen.findByRole('alert')).textContent).toContain(
      'API unreachable (HTTP 500)',
    )
    expect(screen.queryByText(/lighthouse undefined/)).toBeNull()
  })
})
