import { useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { type JSX, useState, useSyncExternalStore } from 'react'
import { formatAgo } from '@/lib/format'
import { useNow } from '@/lib/now'
import { useVersion } from '@/lib/queries'

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

export function Header(): JSX.Element {
  const queryClient = useQueryClient()
  const { data, error } = useVersion()
  const now = useNow()
  const updatedAt = useOldestUpdate()
  // True from the click until every refetch has settled, failed ones included.
  const [refreshing, setRefreshing] = useState(false)
  const refresh = () => {
    setRefreshing(true)
    void queryClient.refetchQueries().finally(() => setRefreshing(false))
  }
  return (
    <header className="flex flex-wrap items-center justify-between gap-4 border-b-4 border-lh-yellow bg-lh-navy px-4 py-3 text-white">
      <div>
        <div className="flex items-center gap-2">
          <h1 className="text-xl font-semibold">lighthouse</h1>
          {data && (
            <span className="rounded-full bg-lh-yellow px-2 py-0.5 text-xs font-medium text-lh-navy" title="version">
              {data}
            </span>
          )}
        </div>
        {error && <p role="alert">API unreachable</p>}
      </div>
      <div className="flex flex-wrap items-center gap-4 text-sm">
        <div className="flex flex-wrap items-center gap-4" aria-live="polite">
        {(updatedAt > 0 || refreshing) && (
          <span>
            {updatedAt > 0 && `updated ${formatAgo(now, updatedAt)}`}
            {updatedAt > 0 && refreshing && ' · '}
            {refreshing && 'refreshing…'}
          </span>
        )}
        </div>
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
