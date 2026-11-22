import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, fetchPriorities, fetchQuestions, fetchRuns, forgeErrorText, saveMilestoneRanks } from './api'

afterEach(() => {
  vi.unstubAllGlobals()
})

function stubResponse(body: string, status: number, contentType: string): void {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(
      new Response(body, { status, headers: { 'Content-Type': contentType } }),
    ),
  )
}

describe('fetchQuestions', () => {
  it('returns the list on 200', async () => {
    stubResponse(
      '{"questions":[{"number":1,"title":"t","question":"q","url":"u","answer_url":"a"}]}',
      200,
      'application/json',
    )
    const questions = await fetchQuestions()
    expect(questions).toHaveLength(1)
    expect(questions[0].number).toBe(1)
  })

  it('returns an empty list when the API sends null', async () => {
    stubResponse('{"questions":null}', 200, 'application/json')
    expect(await fetchQuestions()).toEqual([])
  })

  it('throws ApiError with the status on 503', async () => {
    stubResponse('{"detail":"forge not configured"}', 503, 'application/problem+json')
    const err = await fetchQuestions().catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(503)
    expect((err as ApiError).detail).toBe('forge not configured')
  })

  it('maps a non-JSON body to ApiError(0, "invalid response")', async () => {
    stubResponse('<html>bad gateway</html>', 502, 'text/html')
    const err = await fetchQuestions().catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).detail).toBe('invalid response')
    expect(forgeErrorText(err)).toBe('Forge unreachable: invalid response')
  })

  it('lets a network error through', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    await expect(fetchQuestions()).rejects.toBeInstanceOf(TypeError)
  })
})

describe('fetchRuns', () => {
  it('returns empty lists when the API sends null', async () => {
    stubResponse('{"runs":null,"deferred":null}', 200, 'application/json')
    expect(await fetchRuns()).toEqual({ runs: [], deferred: [], workspaceUrl: '', night: '' })
  })

  it('passes the workspace url and the night window on', async () => {
    stubResponse('{"runs":[],"deferred":[],"workspace_url":"http://h","night":"21:00-08:00"}', 200, 'application/json')
    expect(await fetchRuns()).toMatchObject({ workspaceUrl: 'http://h', night: '21:00-08:00' })
  })
})

describe('fetchPriorities', () => {
  it('returns the repos and runs on 200', async () => {
    stubResponse(
      '{"repos":[{"slug":"o/r","issues":[{"number":1,"title":"t","url":"u","status":"","blocked_by":null}],"milestones":null}],"runs":[{"repo":"o/r","issue":1,"state":"open","phase":"build"}]}',
      200,
      'application/json',
    )
    expect(await fetchPriorities()).toEqual({
      repos: [
        {
          slug: 'o/r',
          issues: [{ number: 1, title: 't', url: 'u', status: '', blocked_by: [] }],
          milestones: [],
        },
      ],
      runs: [{ repo: 'o/r', issue: 1, state: 'open', phase: 'build' }],
    })
  })

  it('returns empty lists when the API sends null', async () => {
    stubResponse('{"repos":null,"runs":null}', 200, 'application/json')
    expect(await fetchPriorities()).toEqual({ repos: [], runs: [] })
  })

  it('throws an ApiError with status and detail on 502', async () => {
    stubResponse('{"detail":"github: 500"}', 502, 'application/problem+json')
    await expect(fetchPriorities()).rejects.toMatchObject({ status: 502, detail: 'github: 500' })
  })
})

describe('forgeErrorText', () => {
  it('explains a missing forge on 503', () => {
    expect(forgeErrorText(new ApiError(503, 'forge not configured'))).toBe(
      'Forge not configured — set GH_TOKEN, and --repo or [dispatch] repos in host.toml',
    )
  })

  it('shows the detail for another status', () => {
    expect(forgeErrorText(new ApiError(502, 'github: 500'))).toBe(
      'Forge unreachable: github: 500',
    )
  })

  it('falls back to the HTTP status when the detail is empty', () => {
    expect(forgeErrorText(new ApiError(502, ''))).toBe('Forge unreachable: HTTP 502')
  })

  it('reports an unreachable server on a TypeError', () => {
    expect(forgeErrorText(new TypeError('Failed to fetch'))).toBe('Server unreachable')
  })
})

describe('saveMilestoneRanks', () => {
  it('posts the changes as JSON to the milestone-ranks endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{"written":[6]}', { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )
    vi.stubGlobal('fetch', fetchMock)
    await saveMilestoneRanks('o/r', [{ number: 6, from: null, to: 1 }])
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/priorities/milestone-ranks')
    expect(init.method).toBe('POST')
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
    expect(new Headers(init.headers).has('Origin')).toBe(false)
    expect(JSON.parse(init.body as string)).toEqual({ repo: 'o/r', changes: [{ number: 6, from: null, to: 1 }] })
  })

  it('throws ApiError(409) with the detail on a stale rank', async () => {
    stubResponse('{"detail":"rank changed"}', 409, 'application/problem+json')
    const err = await saveMilestoneRanks('o/r', [{ number: 6, from: 2, to: 1 }]).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 409, detail: 'rank changed' })
  })
})
