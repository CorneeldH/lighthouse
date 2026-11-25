import { afterEach, describe, expect, it, vi } from 'vitest'
import { loadPrefs, savePrefs } from './prefs'

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('prefs', () => {
  it('returns the defaults when nothing is stored', () => {
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null, msOrder: 'computed', depsOrder: 'prio' })
  })

  it('round-trips view and repos', () => {
    savePrefs({ view: 'milestones', repos: ['a/x'], msOrder: 'priority', depsOrder: 'prio' })
    expect(loadPrefs()).toEqual({ view: 'milestones', repos: ['a/x'], msOrder: 'priority', depsOrder: 'prio' })
  })

  it('falls back to the defaults on garbage or an unknown view', () => {
    localStorage.setItem('lighthouse.priorities', '{nope')
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null, msOrder: 'computed', depsOrder: 'prio' })
    localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'zzz', repos: 'x' }))
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null, msOrder: 'computed', depsOrder: 'prio' })
  })

  it('falls back to computed for an unknown msOrder and keeps a valid view', () => {
    localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'milestones', msOrder: 'zzz' }))
    expect(loadPrefs()).toEqual({ view: 'milestones', repos: null, msOrder: 'computed', depsOrder: 'prio' })
  })

  it('does not throw when localStorage throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(loadPrefs()).toEqual({ view: 'deps', repos: null, msOrder: 'computed', depsOrder: 'prio' })
    expect(() => savePrefs({ view: 'backlog', repos: null, msOrder: 'computed', depsOrder: 'prio' })).not.toThrow()
  })

  it('opens Backlog for a stored view "unassigned"', () => {
    localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'unassigned' }))
    expect(loadPrefs().view).toBe('backlog')
  })

  it('keeps a stored view "backlog"', () => {
    localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'backlog' }))
    expect(loadPrefs().view).toBe('backlog')
  })

  it('depsOrder staat standaard op prio', () => {
    expect(loadPrefs().depsOrder).toBe('prio')
  })

  it('een opgeslagen weight of crossings wordt teruggelezen', () => {
    for (const depsOrder of ['weight', 'crossings'] as const) {
      savePrefs({ view: 'deps', repos: null, msOrder: 'computed', depsOrder })
      expect(loadPrefs().depsOrder).toBe(depsOrder)
    }
  })

  it('een onbekende waarde valt terug op prio', () => {
    for (const bad of ['zzz', 3, null, ['weight']]) {
      localStorage.setItem('lighthouse.priorities', JSON.stringify({ view: 'deps', depsOrder: bad }))
      expect(loadPrefs().depsOrder).toBe('prio')
    }
  })

  it('een kapotte of geblokkeerde localStorage geeft prio', () => {
    localStorage.setItem('lighthouse.priorities', '{nope')
    expect(loadPrefs().depsOrder).toBe('prio')
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(loadPrefs().depsOrder).toBe('prio')
  })
})
