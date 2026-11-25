import type { JSX } from 'react'
import type { Options } from '@/api/lighthouse'
import type { TelemetryFilter } from '@/lib/api'

interface FilterBarProps {
  options: Options
  value: TelemetryFilter
  onChange: (value: TelemetryFilter) => void
}

const FIELDS: { key: keyof TelemetryFilter; label: string; options: keyof Options }[] = [
  { key: 'repo', label: 'Repo', options: 'repos' },
  { key: 'route', label: 'Route', options: 'routes' },
  { key: 'size', label: 'Size', options: 'sizes' },
  { key: 'pr_size', label: 'PR size', options: 'pr_sizes' },
  { key: 'area', label: 'Area', options: 'areas' },
  { key: 'model', label: 'Model', options: 'models' },
  { key: 'harness', label: 'Harness', options: 'harnesses' },
]

export function FilterBar({ options, value, onChange }: FilterBarProps): JSX.Element {
  return (
    <div className="flex flex-wrap gap-3">
      {FIELDS.map((f) => {
        const id = `telemetry-filter-${f.key}`
        return (
          <div key={f.key} className="flex flex-col text-sm">
            <label htmlFor={id}>{f.label}</label>
            <select
              id={id}
              className="rounded-md border bg-background px-2 py-1"
              value={value[f.key] ?? ''}
              onChange={(e) => {
                const next = { ...value }
                if (e.target.value === '') delete next[f.key]
                else next[f.key] = e.target.value
                onChange(next)
              }}
            >
              <option value="">All</option>
              {(options[f.options] ?? []).map((o) => (
                <option key={o} value={o}>
                  {o}
                </option>
              ))}
            </select>
          </div>
        )
      })}
    </div>
  )
}
