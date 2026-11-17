import { type JSX, useCallback, useEffect, useState } from 'react'

export type TabId = 'controller' | 'prioritering'

const TABS: Array<{ id: TabId; label: string }> = [
  { id: 'controller', label: 'Controller' },
  { id: 'prioritering', label: 'Prioritering' },
]

function fromHash(): TabId {
  return window.location.hash === '#prioritering' ? 'prioritering' : 'controller'
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
