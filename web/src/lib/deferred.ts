import type { DeferredView } from '@/api/lighthouse'

// Only the exact reason "night" is a night handoff; every other reason is a brake.
export function splitDeferred(deferred: DeferredView[]): { queue: DeferredView[]; night: DeferredView[] } {
  return {
    queue: deferred.filter((d) => d.reason !== 'night'),
    night: deferred.filter((d) => d.reason === 'night'),
  }
}
