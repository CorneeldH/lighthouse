import { describe, expect, it } from 'vitest'
import { formatAge, formatPct, formatReset } from './format'

describe('formatAge', () => {
  it('rounds down to the unit', () => {
    expect(formatAge(30)).toBe('< 1 min')
    expect(formatAge(60)).toBe('1 min')
    expect(formatAge(720)).toBe('12 min')
    expect(formatAge(3599)).toBe('59 min')
    expect(formatAge(3600)).toBe('1 h')
    expect(formatAge(10800)).toBe('3 h')
  })
})

describe('formatPct', () => {
  it('marks a lower bound', () => {
    expect(formatPct(42, false)).toBe('42%')
    expect(formatPct(42, true)).toBe('≥ 42%')
    expect(formatPct(0, false)).toBe('0%')
  })
})

describe('formatReset', () => {
  // Local-time constructors: the expectations hold in any time zone.
  const now = new Date(2026, 9, 27, 10, 0)

  it('is empty for an empty value', () => {
    expect(formatReset('', now)).toBe('')
  })

  it('shows only the time on the same local day', () => {
    expect(formatReset(new Date(2026, 9, 27, 14, 30).toISOString(), now)).toBe('14:30')
  })

  it('adds the weekday on another day', () => {
    // 2026-10-29 is a Thursday.
    expect(formatReset(new Date(2026, 9, 29, 16, 59).toISOString(), now)).toBe('Thu 16:59')
  })
})
