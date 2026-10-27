import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, fetchQuestions, forgeErrorText } from './api'

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

describe('forgeErrorText', () => {
  it('explains a missing forge on 503', () => {
    expect(forgeErrorText(new ApiError(503, 'forge not configured'))).toBe(
      'Forge not configured — set GH_TOKEN and --repo',
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
