import {
  getHealth,
  getQuestions,
  getReturned,
  getUsage,
  type Question,
  type Report,
  type Returned,
} from '@/api/lighthouse'

export class ApiError extends Error {
  readonly status: number
  readonly detail: string

  constructor(status: number, detail: string) {
    super(detail === '' ? `HTTP ${status}` : detail)
    this.name = 'ApiError'
    this.status = status
    this.detail = detail
  }
}

interface Result {
  status: number
  data: unknown
}

function detailOf(data: unknown): string {
  if (typeof data === 'object' && data !== null && 'detail' in data) {
    const { detail } = data as { detail: unknown }
    if (typeof detail === 'string') return detail
  }
  return ''
}

// The orval functions parse the body before they return the status, so a body
// that is not JSON (a proxy answering with HTML) throws a SyntaxError.
async function call<T extends Result>(request: () => Promise<T>): Promise<T> {
  let res: T
  try {
    res = await request()
  } catch (err) {
    if (err instanceof SyntaxError) throw new ApiError(0, 'invalid response')
    throw err
  }
  if (res.status !== 200) throw new ApiError(res.status, detailOf(res.data))
  return res
}

export async function fetchQuestions(signal?: AbortSignal): Promise<Question[]> {
  const res = await call(() => getQuestions({ signal }))
  return (res.data as { questions: Question[] | null }).questions ?? []
}

export async function fetchReturned(signal?: AbortSignal): Promise<Returned[]> {
  const res = await call(() => getReturned({ signal }))
  return (res.data as { returned: Returned[] | null }).returned ?? []
}

export async function fetchUsage(signal?: AbortSignal): Promise<Report> {
  const res = await call(() => getUsage({ signal }))
  return res.data as Report
}

export async function fetchVersion(signal?: AbortSignal): Promise<string> {
  const res = await call(() => getHealth({ signal }))
  return (res.data as { version: string }).version
}

export function forgeErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 503) return 'Forge not configured — set GH_TOKEN and --repo'
    return `Forge unreachable: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Server unreachable'
}
