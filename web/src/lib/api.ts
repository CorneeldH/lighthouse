import {
  type DeferredView,
  type Leftover,
  getCleanup,
  getHealth,
  getPriorities,
  getQuestions,
  getReturned,
  getTelemetryComponents,
  getTelemetryCoverage,
  getTelemetryTimeline,
  getTelemetryTrajectories,
  getTelemetryTrajectory,
  getRuns,
  getUsage,
  postMilestoneRanks,
  type RankChange,
  type Issue,
  type ComponentsReport,
  type CoverageReport,
  type Detail,
  type ListRow,
  type TimelineReport,
  type Milestone,
  type Question,
  type Repo,
  type Report,
  type Returned,
  type Run,
  type RunView,
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

export async function fetchCleanup(signal?: AbortSignal): Promise<Leftover[]> {
  const res = await call(() => getCleanup({ signal }))
  return (res.data as { cleanup: Leftover[] | null }).cleanup ?? []
}

export interface RunsData {
  runs: RunView[]
  deferred: DeferredView[]
  workspaceUrl: string
  night: string
}

export async function fetchRuns(signal?: AbortSignal): Promise<RunsData> {
  const res = await call(() => getRuns({ signal }))
  const body = res.data as {
    runs: RunView[] | null
    deferred: DeferredView[] | null
    workspace_url?: string
    night?: string
  }
  return {
    runs: body.runs ?? [],
    deferred: body.deferred ?? [],
    workspaceUrl: body.workspace_url ?? '',
    night: body.night ?? '',
  }
}

export async function fetchUsage(signal?: AbortSignal): Promise<Report> {
  const res = await call(() => getUsage({ signal }))
  return res.data as Report
}

export async function fetchVersion(signal?: AbortSignal): Promise<string> {
  const res = await call(() => getHealth({ signal }))
  return (res.data as { version: string }).version
}

export interface PriorityRepo {
  slug: string
  issues: Issue[]
  milestones: Milestone[]
}

export interface PrioritiesData {
  repos: PriorityRepo[]
  runs: Run[]
}

export async function fetchPriorities(signal?: AbortSignal): Promise<PrioritiesData> {
  const res = await call(() => getPriorities({ signal }))
  const body = res.data as { repos: Repo[] | null; runs: Run[] | null }
  return {
    repos: (body.repos ?? []).map((r) => ({
      slug: r.slug,
      issues: (r.issues ?? []).map((i) => ({ ...i, blocked_by: i.blocked_by ?? [] })),
      milestones: r.milestones ?? [],
    })),
    runs: body.runs ?? [],
  }
}

// Writes the ranks of milestones in one repo. A stale `from` comes back as ApiError(409). The
// browser sets the Origin header the server checks; never set it here.
export async function saveMilestoneRanks(repo: string, changes: RankChange[]): Promise<void> {
  await call(() => postMilestoneRanks({ repo, changes }))
}

export function forgeErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 503) return 'Forge not configured — set GH_TOKEN, and --repo or [dispatch] repos in host.toml'
    return `Forge unreachable: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Server unreachable'
}

export type TelemetryFilter = {
  repo?: string
  route?: string
  size?: string
  pr_size?: string
  area?: string
  model?: string
  harness?: string
  component?: string
}

// Drops empty values so an unset filter never reaches the query string.
function clean<K extends keyof TelemetryFilter>(filter: TelemetryFilter, keys: readonly K[]): Partial<Record<K, string>> {
  const out: Partial<Record<K, string>> = {}
  for (const k of keys) {
    const v = filter[k]
    if (v !== undefined && v !== '') out[k] = v
  }
  return out
}

const COMPONENT_KEYS = ['repo', 'route', 'size', 'pr_size', 'area', 'model', 'harness'] as const
const TRAJECTORY_KEYS = [...COMPONENT_KEYS, 'component'] as const

export async function fetchTelemetryComponents(filter: TelemetryFilter, signal?: AbortSignal): Promise<ComponentsReport> {
  const res = await call(() => getTelemetryComponents(clean(filter, COMPONENT_KEYS), { signal }))
  const body = res.data as ComponentsReport
  return { ...body, blocks: body.blocks ?? [] }
}

export async function fetchTelemetryTimeline(filter: TelemetryFilter, signal?: AbortSignal): Promise<TimelineReport> {
  const res = await call(() => getTelemetryTimeline(clean(filter, ['repo'] as const), { signal }))
  const body = res.data as TimelineReport
  return { markers: body.markers ?? [], weeks: body.weeks ?? [] }
}

export async function fetchTelemetryTrajectories(filter: TelemetryFilter, signal?: AbortSignal): Promise<ListRow[]> {
  const res = await call(() => getTelemetryTrajectories(clean(filter, TRAJECTORY_KEYS), { signal }))
  return (res.data as { trajectories: ListRow[] | null }).trajectories ?? []
}

export async function fetchTelemetryTrajectory(id: number, signal?: AbortSignal): Promise<Detail> {
  const res = await call(() => getTelemetryTrajectory(id, { signal }))
  return res.data as Detail
}

export async function fetchTelemetryCoverage(filter: TelemetryFilter, signal?: AbortSignal): Promise<CoverageReport> {
  const res = await call(() => getTelemetryCoverage(clean(filter, ['repo'] as const), { signal }))
  const body = res.data as CoverageReport
  return { ...body, lines: body.lines ?? [] }
}
