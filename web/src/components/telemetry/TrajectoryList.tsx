import { useEffect, useState, type JSX } from 'react'
import type { ListRow } from '@/api/lighthouse'
import { forgeErrorText, type TelemetryFilter } from '@/lib/api'
import { useTelemetryTrajectories } from '@/lib/queries'
import { money } from './format'

// The recovery states of report.recoveryState, as the list shows them.
const RECOVERY_LABELS: Record<string, string> = {
  none_measured: 'not measured',
  open: 'window open',
  recovered: 'recovered',
  none: 'no recovery needed',
}
const recoveryLabel = (r: string): string => RECOVERY_LABELS[r] ?? r

type SortKey = 'repo' | 'issue' | 'size' | 'cost' | 'recovery'
const COLUMNS: SortKey[] = ['repo', 'issue', 'size', 'cost', 'recovery']

function compare(a: ListRow, b: ListRow, key: SortKey, dir: 1 | -1): number {
  if (key === 'cost') {
    // A trajectory without a measured cost sorts last in both directions.
    if (a.cost === null && b.cost === null) return 0
    if (a.cost === null) return 1
    if (b.cost === null) return -1
    return (a.cost - b.cost) * dir
  }
  const x = a[key]
  const y = b[key]
  return (typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y))) * dir
}

export function TrajectoryList({ filter }: { filter: TelemetryFilter }): JSX.Element {
  const [component, setComponent] = useState('')
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 } | null>(null)
  const q = useTelemetryTrajectories({ ...filter, component })
  // Options accumulate, so picking a component does not remove the others.
  const [known, setKnown] = useState<string[]>([])
  const rows = q.data
  useEffect(() => {
    if (!rows) return
    const found = rows.flatMap((r) => r.components ?? [])
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setKnown((prev) => (found.every((c) => prev.includes(c)) ? prev : [...new Set([...prev, ...found])].sort()))
  }, [rows])
  const list = rows ?? []
  const sorted = sort === null ? list : [...list].sort((a, b) => compare(a, b, sort.key, sort.dir))
  const clickSort = (key: SortKey): void =>
    setSort((s) => (s?.key === key ? { key, dir: s.dir === 1 ? -1 : 1 } : { key, dir: 1 }))

  return (
    <div className="space-y-2">
      <label className="flex items-center gap-2 text-sm">
        Component
        <select aria-label="Component" value={component} onChange={(e) => setComponent(e.target.value)} className="rounded border bg-background px-2 py-1">
          <option value="">All</option>
          {known.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
      </label>
      {q.error !== null && q.error !== undefined && <p role="alert">{forgeErrorText(q.error)}</p>}
      {(q.isPending || q.isPlaceholderData) && <p>Loading…</p>}
      {q.data && list.length === 0 && <p>No trajectories</p>}
      {list.length > 0 && (
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left">
              {COLUMNS.map((k) => (
                <th key={k} scope="col" aria-sort={sort?.key === k ? (sort.dir === 1 ? 'ascending' : 'descending') : undefined}>
                  <button type="button" onClick={() => clickSort(k)} className="font-semibold capitalize">
                    {k}
                  </button>
                </th>
              ))}
              <th scope="col">Components</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((r) => (
              <tr key={r.id}>
                <td>
                  <a href={`#telemetry/trajectory/${r.id}`} className="underline">
                    {r.repo}
                  </a>
                </td>
                <td>{r.issue}</td>
                <td>{r.size === '' ? 'not measured' : r.size}</td>
                <td>{r.cost === null ? 'not measured' : `${money(r.cost)} (${r.cost_kind})`}</td>
                <td>{recoveryLabel(r.recovery)}</td>
                <td>{(r.components ?? []).join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
