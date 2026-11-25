import { type JSX, useCallback, useEffect, useState } from 'react'

export type TabId = 'controller' | 'priorities' | 'telemetry'

const TABS: Array<{ id: TabId; label: string }> = [
  { id: 'controller', label: 'Controller' },
  { id: 'priorities', label: 'Priorities' },
  { id: 'telemetry', label: 'Telemetry' },
]

function fromHash(): TabId {
  const hash = window.location.hash
  if (hash === '#priorities') return 'priorities'
  if (hash === '#telemetry' || hash.startsWith('#telemetry/')) return 'telemetry'
  return 'controller'
}

export type TelemetryRoute = { view: 'overview' } | { view: 'trajectory'; id: number }

// The telemetry view the hash asks for; anything but `#telemetry/trajectory/<id>` is the overview.
// Beside the tab bar that owns the hash.
// eslint-disable-next-line react-refresh/only-export-components
export function telemetryRoute(): TelemetryRoute {
  const m = /^#telemetry\/trajectory\/(\d+)$/.exec(window.location.hash)
  return m === null ? { view: 'overview' } : { view: 'trajectory', id: Number(m[1]) }
}

// The hook lives beside the component it drives (one tab bar, one hook).
// eslint-disable-next-line react-refresh/only-export-components
export function useHashTab(): [TabId, (t: TabId) => void] {
  const [tab, setTabState] = useState<TabId>(fromHash)
  useEffect(() => {
    const onChange = () => setTabState(fromHash())
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  const setTab = useCallback((t: TabId) => {
    setTabState(t)
    const hash = t === 'controller' ? '' : `#${t}`
    history.replaceState(null, '', `${window.location.pathname}${window.location.search}${hash}`)
  }, [])
  return [tab, setTab]
}

export function Tabs({ active, onChange }: { active: TabId; onChange: (t: TabId) => void }): JSX.Element {
  return (
    <div role="tablist" aria-label="Views" className="flex gap-1 border-b-2 border-lh-navy">
      {TABS.map((t) => {
        const on = t.id === active
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={on}
            className={`-mb-0.5 border-b-4 px-4 py-2 text-sm font-medium ${
              on ? 'border-lh-yellow text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'
            }`}
            onClick={() => onChange(t.id)}
          >
            {t.label}
          </button>
        )
      })}
    </div>
  )
}
