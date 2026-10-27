import { useQueryClient } from '@tanstack/react-query'
import type { JSX } from 'react'
import { useVersion } from '@/lib/queries'

export function Header(): JSX.Element {
  const queryClient = useQueryClient()
  const { data, error } = useVersion()
  return (
    <header className="flex items-center justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold">{data ? `lighthouse ${data}` : 'lighthouse'}</h1>
        {error && <p role="alert">API unreachable</p>}
      </div>
      <button
        type="button"
        className="rounded-md border px-3 py-1 text-sm hover:bg-muted"
        onClick={() => void queryClient.refetchQueries()}
      >
        Refresh
      </button>
    </header>
  )
}
