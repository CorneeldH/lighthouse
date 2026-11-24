import type { DepsOrder } from '@/components/priorities/layout'

export type ViewId = 'deps' | 'milestones' | 'unassigned'

export interface Prefs {
  view: ViewId
  // null = every repo on.
  repos: string[] | null
  msOrder: 'computed' | 'priority'
  depsOrder: DepsOrder
}

const KEY = 'lighthouse.priorities'
const DEPS_ORDERS: readonly DepsOrder[] = ['prio', 'weight', 'crossings']
const VIEWS: readonly ViewId[] = ['deps', 'milestones', 'unassigned']

export function loadPrefs(): Prefs {
  const out: Prefs = { view: 'deps', repos: null, msOrder: 'computed', depsOrder: 'prio' }
  try {
    const raw = localStorage.getItem(KEY)
    if (raw === null) return out
    const p = JSON.parse(raw) as { view?: unknown; repos?: unknown; msOrder?: unknown; depsOrder?: unknown }
    if (VIEWS.includes(p.view as ViewId)) out.view = p.view as ViewId
    if (p.msOrder === 'computed' || p.msOrder === 'priority') out.msOrder = p.msOrder
    if (DEPS_ORDERS.includes(p.depsOrder as DepsOrder)) out.depsOrder = p.depsOrder as DepsOrder
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
