import type { JSX } from 'react'

const REGIONS = ['Components', 'Timeline', 'Trajectories', 'Coverage']

export function TelemetryTab(): JSX.Element {
  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold">Telemetry</h2>
      {REGIONS.map((name) => (
        <section key={name} aria-label={name} />
      ))}
    </div>
  )
}
