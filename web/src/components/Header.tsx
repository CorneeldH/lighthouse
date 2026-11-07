import { useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { type JSX, useState } from 'react'
import { useVersion } from '@/lib/queries'

export function Header(): JSX.Element {
  const queryClient = useQueryClient()
  const { data, error } = useVersion()
  // True from the click until every refetch has settled, failed ones included.
  const [refreshing, setRefreshing] = useState(false)
  const refresh = () => {
    setRefreshing(true)
    void queryClient.refetchQueries().finally(() => setRefreshing(false))
  }
  return (
    <header className="flex items-center justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold">{data ? `lighthouse ${data}` : 'lighthouse'}</h1>
        {error && <p role="alert">API unreachable</p>}
      </div>
      <button
        type="button"
        className="inline-flex items-center gap-2 rounded-md border px-3 py-1 text-sm hover:bg-muted disabled:opacity-60"
        onClick={refresh}
        disabled={refreshing}
        aria-busy={refreshing}
      >
        <RefreshCw aria-hidden className={refreshing ? 'size-4 animate-spin' : 'size-4'} />
        {refreshing ? 'Refreshing…' : 'Refresh'}
      </button>
    </header>
  )
}
