import type { JSX } from 'react'
import { useCleanup } from '@/lib/queries'
import { Panel } from './Panel'

export function CleanupList(): JSX.Element {
  const { data, error, isPending, dataUpdatedAt } = useCleanup()
  return (
    <Panel
      title="Workspaces to clean up"
      updatedAt={dataUpdatedAt}
      error={error}
      loading={isPending && !error}
    >
      {data && data.length === 0 && <p>No workspaces to clean up</p>}
      {data && data.length > 0 && (
        <ul className="space-y-4">
          {data.map((l) => (
            <li key={`${l.repo}/${l.workspace_id}`} className="space-y-1">
              <p>
                <span>
                  {l.repo}#{l.issue}
                </span>{' '}
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
