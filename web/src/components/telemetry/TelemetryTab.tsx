import { useState, type JSX } from 'react'
import type { TelemetryFilter } from '@/lib/api'
import { ComponentsView } from './ComponentsView'
import { CoverageView } from './CoverageView'
import { TimelineView } from './TimelineView'

const PLACEHOLDERS = ['Trajectories']

export function TelemetryTab(): JSX.Element {
  const [filter, setFilter] = useState<TelemetryFilter>({})
  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold">Telemetry</h2>
      <section aria-label="Components">
        <ComponentsView filter={filter} onFilterChange={setFilter} />
      </section>
      <section aria-label="Timeline">
        <TimelineView filter={filter} />
      </section>
      {PLACEHOLDERS.map((name) => (
        <section key={name} aria-label={name} />
      ))}
      <section aria-label="Coverage">
        <CoverageView filter={filter} />
      </section>
    </div>
  )
}
