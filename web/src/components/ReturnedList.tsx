import { Undo2 } from 'lucide-react'
import type { JSX } from 'react'
import { useReturned } from '@/lib/queries'
import { IssueLink } from './IssueLink'
import { Panel } from './Panel'

export function ReturnedList(): JSX.Element {
  const { data, error, isPending } = useReturned()
  return (
    <Panel id="returned" title="Returned" icon={Undo2} tone="orange" count={data?.length}
      error={error} loading={isPending && !error}
    >
      {data && data.length === 0 && <p>No returned issues</p>}
      {data && data.length > 0 && (
        <ul className="space-y-4">
          {data.map((r) => (
            <li key={`${r.repo}#${r.number}`} className="space-y-1">
              <p>
                <IssueLink repo={r.repo} issue={r.number} />{' '}
                <IssueLink repo={r.repo} issue={r.number}>
                  {r.title}
                </IssueLink>
              </p>
              <p>{r.reason}</p>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}
