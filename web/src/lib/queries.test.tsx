import { focusManager, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { newQueryClient, USAGE_INTERVAL_MS, useQuestions, useUsage } from './queries'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  focusManager.setFocused(undefined)
})

describe('useUsage', () => {
  it('polls /api/usage on the usage interval', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response('{}', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = newQueryClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )

    renderHook(() => useUsage(), { wrapper })

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[0][0]).toBe('/api/usage')

    await vi.advanceTimersByTimeAsync(USAGE_INTERVAL_MS)

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(fetchMock.mock.calls[1][0]).toBe('/api/usage')
    client.clear()
  })
})

describe('useQuestions', () => {
  it('does not refetch on tab focus within the interval', async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response('{"questions":[]}', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = newQueryClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )

    const { result } = renderHook(() => useQuestions(), { wrapper })
    // Wait for the data, not the call: a focus refetch during an in-flight fetch is a no-op.
    await vi.waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/questions')

    focusManager.setFocused(false)
    focusManager.setFocused(true)
    await new Promise((resolve) => setTimeout(resolve, 50))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    client.clear()
  })
})
