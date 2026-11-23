import { Monitor, Moon, Sun } from 'lucide-react'
import { type JSX, useEffect, useState } from 'react'
import { applyTheme, loadTheme, saveTheme, type ThemeMode } from '@/lib/theme'

const MODES: { mode: ThemeMode; label: string; Icon: typeof Monitor }[] = [
  { mode: 'system', label: 'System', Icon: Monitor },
  { mode: 'light', label: 'Light', Icon: Sun },
  { mode: 'dark', label: 'Dark', Icon: Moon },
]

export function ThemeToggle(): JSX.Element {
  const [mode, setMode] = useState<ThemeMode>(loadTheme)
  useEffect(() => applyTheme(mode), [mode])
  const pick = (next: ThemeMode) => {
    setMode(next)
    saveTheme(next)
  }
  return (
    <div role="group" aria-label="Theme" className="inline-flex overflow-hidden rounded-md border border-white/40">
      {MODES.map(({ mode: m, label, Icon }) => (
        <button
          key={m}
          type="button"
          aria-pressed={mode === m}
          onClick={() => pick(m)}
          className={
            mode === m
              ? 'inline-flex items-center gap-1 bg-lh-yellow px-2 py-1 text-sm font-medium text-lh-navy'
              : 'inline-flex items-center gap-1 px-2 py-1 text-sm text-white hover:bg-white/10'
          }
        >
          <Icon aria-hidden className="size-4" />
          {label}
        </button>
      ))}
    </div>
  )
}
