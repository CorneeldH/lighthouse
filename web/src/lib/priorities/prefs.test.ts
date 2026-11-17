import { afterEach, describe, expect, it, vi } from 'vitest'
import { loadPrefs, savePrefs } from './prefs'

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('prefs', () => {
  it('returns the defaults when nothing is stored', () => {
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null })
  })

  it('round-trips view and repos', () => {
    savePrefs({ view: 'milestones', repos: ['a/x'] })
    expect(loadPrefs()).toEqual({ view: 'milestones', repos: ['a/x'] })
  })

  it('falls back to the defaults on garbage or an unknown view', () => {
    localStorage.setItem('lighthouse.priorities', '{nope')
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null })
    localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'zzz', repos: 'x' }))
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null })
  })

  it('does not throw when localStorage throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null })
    expect(() => savePrefs({ view: 'unassigned', repos: null })).not.toThrow()
  })
})
