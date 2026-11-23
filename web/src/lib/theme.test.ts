import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyTheme, loadTheme, saveTheme, THEME_KEY } from './theme'

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
  delete document.documentElement.dataset.theme
})

describe('theme', () => {
  it('defaults to system without storage', () => {
    expect(loadTheme()).toBe('system')
  })

  it('reads back a saved mode', () => {
    saveTheme('dark')
    expect(localStorage.getItem(THEME_KEY)).toBe('dark')
    expect(loadTheme()).toBe('dark')
  })

  it('falls back to system on an unknown value', () => {
    localStorage.setItem(THEME_KEY, 'purple')
    expect(loadTheme()).toBe('system')
  })

  it('falls back to system when localStorage throws', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked')
      },
      setItem: () => {
        throw new Error('blocked')
      },
    })
    expect(loadTheme()).toBe('system')
    expect(() => saveTheme('dark')).not.toThrow()
  })

  it('sets data-theme for light and dark and removes it for system', () => {
    applyTheme('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    applyTheme('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    applyTheme('system')
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })
})
