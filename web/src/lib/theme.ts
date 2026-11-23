export type ThemeMode = 'system' | 'light' | 'dark'

export const THEME_KEY = 'lighthouse.theme'

export function loadTheme(): ThemeMode {
  try {
    const raw = localStorage.getItem(THEME_KEY)
    if (raw === 'light' || raw === 'dark') return raw
  } catch {
    // localStorage blocked: follow the system.
  }
  return 'system'
}

export function saveTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(THEME_KEY, mode)
  } catch {
    // Not persisting is fine.
  }
}

// light and dark force the scheme through data-theme on <html>; system removes it so prefers-color-scheme decides.
export function applyTheme(mode: ThemeMode): void {
  if (mode === 'system') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = mode
}
