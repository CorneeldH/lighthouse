import type { JSX } from 'react'
import { ApiError } from '@/lib/api'
import { useCleanup } from '@/lib/queries'
import { IssueLink } from './IssueLink'
import { Panel } from './Panel'

function cleanupErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    return `Cleanup list unavailable: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Server unreachable'
}

export function CleanupList(): JSX.Element {
  const { data, error, isPending, isFetching, dataUpdatedAt } = useCleanup()
  return (
    <Panel
      title="Workspaces to clean up"
      updatedAt={dataUpdatedAt}
      error={error}
      errorText={cleanupErrorText}
      loading={isPending && !error}
      refreshing={isFetching && !isPending}
    >
      {data && data.length === 0 && <p>No workspaces to clean up</p>}
      {data && data.length > 0 && (
        <ul className="space-y-4">
          {data.map((l) => (
            <li key={`${l.repo}/${l.workspace_id}`} className="space-y-1">
              <p>
                <IssueLink repo={l.repo} issue={l.issue} />{' '}
                <span>{l.workspace_id}</span>
              </p>
              <p>
                <code>{l.path}</code>
              </p>
              <p>{l.reason}</p>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}
