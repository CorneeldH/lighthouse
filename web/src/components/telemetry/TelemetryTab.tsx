import { useState, type JSX } from 'react'
import type { TelemetryFilter } from '@/lib/api'
import { ComponentsView } from './ComponentsView'

const PLACEHOLDERS = ['Timeline', 'Trajectories', 'Coverage']

export function TelemetryTab(): JSX.Element {
  const [filter, setFilter] = useState<TelemetryFilter>({})
  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold">Telemetry</h2>
      <section aria-label="Components">
        <ComponentsView filter={filter} onFilterChange={setFilter} />
      </section>
      {PLACEHOLDERS.map((name) => (
        <section key={name} aria-label={name} />
      ))}
    </div>
  )
}
