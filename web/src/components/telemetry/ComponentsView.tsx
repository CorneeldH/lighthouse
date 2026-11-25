import type { JSX } from 'react'
import { forgeErrorText, type TelemetryFilter } from '@/lib/api'
import { useTelemetryComponents } from '@/lib/queries'
import { ComparisonBlock } from './ComparisonBlock'
import { FilterBar } from './FilterBar'

interface ComponentsViewProps {
  filter: TelemetryFilter
  onFilterChange: (filter: TelemetryFilter) => void
}

export function ComponentsView({ filter, onFilterChange }: ComponentsViewProps): JSX.Element {
  const q = useTelemetryComponents(filter)
  return (
    <div className="space-y-3">
      {q.error !== null && q.error !== undefined && <p role="alert">{forgeErrorText(q.error)}</p>}
      {(q.isPending || q.isPlaceholderData) && <p>Loading…</p>}
      {q.data && <FilterBar options={q.data.options} value={filter} onChange={onFilterChange} />}
      {q.data && (q.data.blocks ?? []).map((b) => <ComparisonBlock key={b.name} block={b} />)}
    </div>
  )
}
