import { useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { type JSX, useState, useSyncExternalStore } from 'react'
import { formatAge } from '@/lib/format'
import { useNow } from '@/lib/now'
import { useRuns, useVersion } from '@/lib/queries'

// The oldest dataUpdatedAt > 0 over every query in the cache; 0 when none has data.
function useOldestUpdate(): number {
  const cache = useQueryClient().getQueryCache()
  return useSyncExternalStore(
    (notify) => cache.subscribe(notify),
    () => {
      const stamps = cache
        .getAll()
        .map((q) => q.state.dataUpdatedAt)
        .filter((t) => t > 0)
      return stamps.length === 0 ? 0 : Math.min(...stamps)
    },
  )
}

function age(now: number, then: number): string {
  return `${formatAge(Math.max(0, (now - then) / 1000))} ago`
}

export function Header(): JSX.Element {
  const queryClient = useQueryClient()
  const { data, error } = useVersion()
  const { data: runs } = useRuns()
  const now = useNow()
  const updatedAt = useOldestUpdate()
  const ticks = [...(runs?.runs ?? []), ...(runs?.deferred ?? [])].map((r) => Date.parse(r.at)).filter((t) => !Number.isNaN(t))
  const tickAt = ticks.length === 0 ? 0 : Math.max(...ticks)
  // True from the click until every refetch has settled, failed ones included.
  const [refreshing, setRefreshing] = useState(false)
  const refresh = () => {
    setRefreshing(true)
    void queryClient.refetchQueries().finally(() => setRefreshing(false))
  }
  return (
    <header className="flex flex-wrap items-center justify-between gap-4 border-b-4 border-lh-yellow bg-lh-navy px-4 py-3 text-white">
      <div>
        <h1 className="text-xl font-semibold">
          lighthouse
          {data && <span className="ml-2 text-base font-normal opacity-80">{data}</span>}
        </h1>
        {error && <p role="alert">API unreachable</p>}
      </div>
      <div className="flex flex-wrap items-center gap-4 text-sm" aria-live="polite">
        {(updatedAt > 0 || refreshing) && (
          <span>
            {updatedAt > 0 && `updated ${age(now, updatedAt)}`}
            {updatedAt > 0 && refreshing && ' · '}
            {refreshing && 'refreshing…'}
          </span>
        )}
        {tickAt > 0 && <span>{`tick ${age(now, tickAt)}`}</span>}
        <button
          type="button"
          className="inline-flex items-center gap-2 rounded-md bg-lh-yellow px-3 py-1 text-sm font-medium text-lh-navy hover:brightness-95 disabled:opacity-60"
          onClick={refresh}
          disabled={refreshing}
          aria-busy={refreshing}
        >
          <RefreshCw aria-hidden className={refreshing ? 'size-4 animate-spin' : 'size-4'} />
          {refreshing ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
    </header>
  )
}
