export type ViewId = 'deps' | 'milestones' | 'unassigned'

export interface Prefs {
  view: ViewId
  // null = every repo on.
  repos: string[] | null
}

const KEY = 'lighthouse.priorities'
const VIEWS: readonly ViewId[] = ['deps', 'milestones', 'unassigned']

export function loadPrefs(): Prefs {
  const out: Prefs = { view: 'deps', repos: null }
  try {
    const raw = localStorage.getItem(KEY)
    if (raw === null) return out
    const p = JSON.parse(raw) as { view?: unknown; repos?: unknown }
    if (VIEWS.includes(p.view as ViewId)) out.view = p.view as ViewId
    if (Array.isArray(p.repos) && p.repos.every((r) => typeof r === 'string')) out.repos = p.repos as string[]
  } catch {
    // localStorage blocked or corrupt: the defaults stand.
  }
  return out
}

export function savePrefs(p: Prefs): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(p))
  } catch {
    // Not persisting is fine.
  }
}
