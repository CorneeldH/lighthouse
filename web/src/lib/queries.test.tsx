import { focusManager, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  newQueryClient,
  QUESTIONS_INTERVAL_MS,
  USAGE_INTERVAL_MS,
  useQuestions,
  useReturned,
  useUsage,
} from './queries'

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

describe('forge page polling', () => {
  it.each([
    ['useQuestions', '/api/questions', useQuestions],
    ['useReturned', '/api/returned', useReturned],
  ])('%s refetches %s after the questions interval', async (_name, path, hook) => {
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

    renderHook(() => hook(), { wrapper })

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[0][0]).toBe(path)

    await vi.advanceTimersByTimeAsync(QUESTIONS_INTERVAL_MS)

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(fetchMock.mock.calls[1][0]).toBe(path)
    client.clear()
  })
})

describe('forge page staleTime', () => {
  it.each([
    ['useQuestions', useQuestions],
    ['useReturned', useReturned],
  ])('%s refetches on remount only after the questions interval', async (_name, hook) => {
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

    // Unmounted, nothing polls: only staleTime decides what a remount does.
    const first = renderHook(() => hook(), { wrapper })
    await vi.waitFor(() => expect(first.result.current.isSuccess).toBe(true))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    first.unmount()

    await vi.advanceTimersByTimeAsync(QUESTIONS_INTERVAL_MS - 5_000)
    const early = renderHook(() => hook(), { wrapper })
    await vi.advanceTimersByTimeAsync(100)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    early.unmount()

    await vi.advanceTimersByTimeAsync(10_000)
    const late = renderHook(() => hook(), { wrapper })
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    late.unmount()
    client.clear()
  })
})
