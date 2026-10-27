import type { JSX } from 'react'
import { useReturned } from '@/lib/queries'
import { Panel } from './Panel'

export function ReturnedList(): JSX.Element {
  const { data, error, isPending, dataUpdatedAt } = useReturned()
  return (
    <Panel title="Returned" updatedAt={dataUpdatedAt} error={error} loading={isPending && !error}>
      {data && data.length === 0 && <p>No returned issues</p>}
      {data && data.length > 0 && (
        <ul className="space-y-4">
          {data.map((r) => (
            <li key={r.number} className="space-y-1">
              <p>
                <span>#{r.number}</span>{' '}
                <a href={r.url} className="underline">
                  {r.title}
                </a>
              </p>
              <p>{r.reason}</p>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}
