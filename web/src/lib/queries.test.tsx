import { focusManager, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  FORGE_INTERVAL_MS,
  newQueryClient,
  QUESTIONS_INTERVAL_MS,
  queryKeys,
  RANK_CONFLICT_TEXT,
  USAGE_INTERVAL_MS,
  usePriorities,
  useQuestions,
  useReturned,
  useTelemetryComponents,
  useTelemetryTrajectories,
  useTelemetryTrajectory,
  useMilestoneRanks,
  useUsage,
} from './queries'
import type { PrioritiesData } from './api'

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

describe('usePriorities', () => {
  it('polls /api/priorities on the forge interval', async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response('{"repos":[],"runs":[]}', {
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

    renderHook(() => usePriorities(), { wrapper })

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[0][0]).toBe('/api/priorities')
    expect(client.getQueryCache().find({ queryKey: ['priorities'] })?.observers[0].options.refetchInterval).toBe(
      FORGE_INTERVAL_MS,
    )
  })
})

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function prioritiesBody(descriptions: Record<string, string>) {
  return {
    repos: Object.entries(descriptions).map(([slug, description]) => ({
      slug,
      issues: [],
      milestones: [{ number: 6, title: 'M', description, open: 1, done: 0 }],
    })),
    runs: [],
  }
}

// A fetch stub that answers GET /api/priorities and POST /api/priorities/milestone-ranks from
// queues the test controls; a POST answer is a function so the test decides when it resolves.
function routeFetch() {
  const gets: Array<() => Promise<Response>> = []
  const posts: Array<{ repo: string; resolve: (r: Response) => void }> = []
  const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    if (url === '/api/priorities') {
      const next = gets.shift()
      if (next === undefined) throw new Error('unexpected GET /api/priorities')
      return next()
    }
    if (url === '/api/priorities/milestone-ranks' && init?.method === 'POST') {
      const { repo } = JSON.parse(init.body as string) as { repo: string }
      return new Promise<Response>((resolve) => posts.push({ repo, resolve }))
    }
    throw new Error(`unexpected fetch ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return { gets, posts, fetchMock }
}

function descriptionIn(data: PrioritiesData | undefined, slug: string): string | undefined {
  return data?.repos.find((r) => r.slug === slug)?.milestones[0]?.description
}

function renderRanks() {
  const client = newQueryClient()
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const view = renderHook(() => ({ priorities: usePriorities(), ranks: useMilestoneRanks() }), { wrapper })
  return { client, ...view }
}

describe('useMilestoneRanks', () => {
  it('shows the new description at once and keeps it after a successful write', async () => {
    const { gets, posts } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Pitch\nPriority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    act(() => result.current.ranks.mutate({ repo: 'o/r', changes: [{ number: 6, from: 2, to: 1 }] }))

    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/r')).toBe('Pitch\nPriority: 1')
    expect(result.current.ranks.isPending('o/r')).toBe(true)

    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Pitch\nPriority: 1' }))))
    await act(async () => posts[0].resolve(json({ written: [6] })))

    await vi.waitFor(() => expect(result.current.ranks.isPending('o/r')).toBe(false))
    expect(result.current.ranks.error).toBeNull()
    expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/r')).toBe('Pitch\nPriority: 1')
    client.clear()
  })

  it('restores fresh data and shows the fixed text on 409', async () => {
    const { gets, posts, fetchMock } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Pitch\nPriority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    act(() => result.current.ranks.mutate({ repo: 'o/r', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/r')).toBe('Pitch\nPriority: 1')

    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Pitch\nPriority: 3' }))))
    await act(async () => posts[0].resolve(json({ detail: 'rank changed' }, 409)))

    await vi.waitFor(() =>
      expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/r')).toBe('Pitch\nPriority: 3'),
    )
    expect(result.current.ranks.error).toBe(RANK_CONFLICT_TEXT)
    expect(RANK_CONFLICT_TEXT).toBe('The order was just changed elsewhere; the list has been refreshed.')
    expect(result.current.ranks.isPending('o/r')).toBe(false)
    expect(fetchMock.mock.calls.filter(([url]) => url === '/api/priorities')).toHaveLength(2)
    client.clear()
  })

  it('shows the server text for another error', async () => {
    const { gets, posts } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Priority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    act(() => result.current.ranks.mutate({ repo: 'o/r', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Priority: 2' }))))
    await act(async () => posts[0].resolve(json({ detail: 'github: 500' }, 502)))

    await vi.waitFor(() => expect(result.current.ranks.error).toBe('Save failed: github: 500'))
    await vi.waitFor(() =>
      expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/r')).toBe('Priority: 2'),
    )
    client.clear()
  })

  it('cancels a running priorities fetch before the optimistic edit', async () => {
    const { gets, posts, fetchMock } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Priority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    // A slow poll that would land after the click with the old description.
    let finishPoll: (r: Response) => void = () => {}
    gets.push(() => new Promise<Response>((resolve) => (finishPoll = resolve)))
    void client.refetchQueries({ queryKey: queryKeys.priorities })
    await vi.waitFor(() => expect(fetchMock.mock.calls.filter(([url]) => url === '/api/priorities')).toHaveLength(2))

    act(() => result.current.ranks.mutate({ repo: 'o/r', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    await act(async () => finishPoll(json(prioritiesBody({ 'o/r': 'Priority: 2' }))))

    expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/r')).toBe('Priority: 1')
    client.clear()
  })

  it('tracks pending per repo while two repos are in flight', async () => {
    const { gets, posts } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/a': 'Priority: 2', 'o/b': 'Priority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    act(() => result.current.ranks.mutate({ repo: 'o/a', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(result.current.ranks.isPending('o/a')).toBe(true)
    expect(result.current.ranks.isPending('o/b')).toBe(false)

    act(() => result.current.ranks.mutate({ repo: 'o/b', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(2))
    expect(result.current.ranks.isPending('o/a')).toBe(true)
    expect(result.current.ranks.isPending('o/b')).toBe(true)

    // B settles first; A is still running and stays pending. The server has not written A yet,
    // so a refetch now would wipe the optimistic rank of A: it waits for A.
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/a': 'Priority: 2', 'o/b': 'Priority: 1' }))))
    await act(async () => posts[1].resolve(json({ written: [6] })))
    await vi.waitFor(() => expect(result.current.ranks.isPending('o/b')).toBe(false))
    await act(() => new Promise((resolve) => setTimeout(resolve, 20)))
    expect(result.current.ranks.isPending('o/a')).toBe(true)
    expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/a')).toBe('Priority: 1')

    gets.length = 0
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/a': 'Priority: 1', 'o/b': 'Priority: 1' }))))
    await act(async () => posts[0].resolve(json({ written: [6] })))
    await vi.waitFor(() => expect(result.current.ranks.isPending('o/a')).toBe(false))
    await vi.waitFor(() => expect(gets).toHaveLength(0))
    client.clear()
  })
  it('rolls back a failed repo at once while another repo is still in flight', async () => {
    const { gets, posts, fetchMock } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/a': 'Priority: 2', 'o/b': 'Priority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    act(() => result.current.ranks.mutate({ repo: 'o/a', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    act(() => result.current.ranks.mutate({ repo: 'o/b', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(2))

    await act(async () => posts[0].resolve(json({ detail: 'rank changed' }, 409)))
    await vi.waitFor(() => expect(result.current.ranks.error).toBe(RANK_CONFLICT_TEXT))

    // No refetch yet (B still runs), but A is back on the server state and B keeps its edit.
    expect(fetchMock.mock.calls.filter(([url]) => url === '/api/priorities')).toHaveLength(1)
    const data = client.getQueryData<PrioritiesData>(queryKeys.priorities)
    expect(descriptionIn(data, 'o/a')).toBe('Priority: 2')
    expect(descriptionIn(data, 'o/b')).toBe('Priority: 1')
    expect(result.current.ranks.isPending('o/b')).toBe(true)

    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/a': 'Priority: 2', 'o/b': 'Priority: 1' }))))
    await act(async () => posts[1].resolve(json({ written: [6] })))
    await vi.waitFor(() => expect(result.current.ranks.isPending('o/b')).toBe(false))
    client.clear()
  })

  it('keeps a repo pending until the refetch after its write has landed', async () => {
    const { gets, posts } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Priority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    act(() => result.current.ranks.mutate({ repo: 'o/r', changes: [{ number: 6, from: 2, to: 1 }] }))
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    let finishRefetch: (r: Response) => void = () => {}
    gets.push(() => new Promise<Response>((resolve) => (finishRefetch = resolve)))
    await act(async () => posts[0].resolve(json({ written: [6] })))
    await vi.waitFor(() => expect(gets).toHaveLength(0))
    await act(() => new Promise((resolve) => setTimeout(resolve, 20)))
    expect(result.current.ranks.isPending('o/r')).toBe(true)

    await act(async () => finishRefetch(json(prioritiesBody({ 'o/r': 'Priority: 1' }))))
    await vi.waitFor(() => expect(result.current.ranks.isPending('o/r')).toBe(false))
    client.clear()
  })

  it('ignores a second click on the same repo in the same tick', async () => {
    const { gets, posts, fetchMock } = routeFetch()
    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Priority: 2' }))))
    const { client, result } = renderRanks()
    await vi.waitFor(() => expect(result.current.priorities.isSuccess).toBe(true))

    act(() => {
      result.current.ranks.mutate({ repo: 'o/r', changes: [{ number: 6, from: 2, to: 1 }] })
      result.current.ranks.mutate({ repo: 'o/r', changes: [{ number: 6, from: 2, to: 3 }] })
    })
    await vi.waitFor(() => expect(posts).toHaveLength(1))
    await act(() => new Promise((resolve) => setTimeout(resolve, 20)))
    expect(fetchMock.mock.calls.filter(([url]) => url === '/api/priorities/milestone-ranks')).toHaveLength(1)
    expect(descriptionIn(client.getQueryData(queryKeys.priorities), 'o/r')).toBe('Priority: 1')

    gets.push(() => Promise.resolve(json(prioritiesBody({ 'o/r': 'Priority: 1' }))))
    await act(async () => posts[0].resolve(json({ written: [6] })))
    await vi.waitFor(() => expect(result.current.ranks.isPending('o/r')).toBe(false))
    client.clear()
  })
})

describe('telemetry hooks', () => {
  function setup(body: string) {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(new Response(body, { status: 200, headers: { 'Content-Type': 'application/json' } })),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = newQueryClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    return { fetchMock, client, wrapper }
  }

  it('useTelemetryComponents encodes the filter and omits empty values', async () => {
    const { fetchMock, client, wrapper } = setup('{"blocks":null,"options":{}}')
    renderHook(() => useTelemetryComponents({ repo: 'a/b', route: '2', size: '' }), { wrapper })
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[0][0]).toBe('/api/telemetry/components?repo=a%2Fb&route=2')
    client.clear()
  })

  it('useTelemetryTrajectories turns a null list into []', async () => {
    const { fetchMock, client, wrapper } = setup('{"trajectories":null}')
    const { result } = renderHook(() => useTelemetryTrajectories({}), { wrapper })
    await vi.waitFor(() => expect(result.current.data).toEqual([]))
    expect(fetchMock.mock.calls[0][0]).toBe('/api/telemetry/trajectories')
    client.clear()
  })

  it('useTelemetryTrajectory fetches one trajectory by id', async () => {
    const { fetchMock, client, wrapper } = setup('{"id":7}')
    renderHook(() => useTelemetryTrajectory(7), { wrapper })
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[0][0]).toBe('/api/telemetry/trajectories/7')
    client.clear()
  })
})
