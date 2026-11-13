import { Trash2 } from 'lucide-react'
import type { JSX } from 'react'
import { ApiError } from '@/lib/api'
import { useCleanup, useRuns } from '@/lib/queries'
import { IssueLink } from './IssueLink'
import { Panel } from './Panel'
import { WorkspaceLink } from './WorkspaceLink'

function cleanupErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    return `Cleanup list unavailable: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Server unreachable'
}

export function CleanupList(): JSX.Element {
  const { data, error, isPending } = useCleanup()
  // The forge url rides along with /api/runs; without it the id stays plain text.
  const { data: runs } = useRuns()
  return (
    <Panel
      id="cleanup"
      title="Workspaces to clean up"
      icon={Trash2}
      tone="green"
      count={data?.length}
      error={error}
      errorText={cleanupErrorText}
      loading={isPending && !error}
    >
      {data && data.length === 0 && <p>No workspaces to clean up</p>}
      {data && data.length > 0 && (
        <ul className="space-y-4">
          {data.map((l) => (
            <li key={`${l.repo}/${l.workspace_id}`} className="space-y-1">
              <p>
                <IssueLink repo={l.repo} issue={l.issue} />{' '}
                <WorkspaceLink id={l.workspace_id} forgeUrl={runs?.workspaceUrl ?? ''} />
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
