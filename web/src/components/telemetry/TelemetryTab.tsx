import { useEffect, useState, type JSX } from 'react'
import type { TelemetryFilter } from '@/lib/api'
import { telemetryRoute, type TelemetryRoute } from '@/components/Tabs'
import { ComponentsView } from './ComponentsView'
import { CoverageView } from './CoverageView'
import { TimelineView } from './TimelineView'
import { TrajectoryDetail } from './TrajectoryDetail'
import { TrajectoryList } from './TrajectoryList'

export function TelemetryTab(): JSX.Element {
  const [filter, setFilter] = useState<TelemetryFilter>({})
  const [route, setRoute] = useState<TelemetryRoute>(telemetryRoute)
  useEffect(() => {
    const onChange = (): void => setRoute(telemetryRoute())
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  if (route.view === 'trajectory') {
    return (
      <div className="space-y-6">
        <h2 className="text-lg font-semibold">Telemetry</h2>
        <TrajectoryDetail id={route.id} />
      </div>
    )
  }
  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold">Telemetry</h2>
      <section aria-label="Components">
        <ComponentsView filter={filter} onFilterChange={setFilter} />
      </section>
      <section aria-label="Timeline">
        <TimelineView filter={filter} />
      </section>
      <section aria-label="Trajectories">
        <h3 className="font-semibold">Trajectories</h3>
        <TrajectoryList filter={filter} />
      </section>
      <section aria-label="Coverage">
        <CoverageView filter={filter} />
      </section>
    </div>
  )
}
