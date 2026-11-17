import { describe, expect, it } from 'vitest'
import { assignColors, NO_MILESTONE_COLOR, PALETTE } from './palette'

describe('PALETTE', () => {
  it('has eight colours and none of the reserved status colours', () => {
    expect(PALETTE).toHaveLength(8)
    for (const reserved of ['--lh-yellow', '--lh-grey', '--lh-navy']) {
      expect(PALETTE.some((c) => c.includes(reserved))).toBe(false)
    }
    expect(PALETTE).not.toContain(NO_MILESTONE_COLOR)
  })
})

describe('assignColors', () => {
  it('colours by (repo, number) order, not input order', () => {
    const a = assignColors([
      { repo: 'b/y', number: 1 },
      { repo: 'a/x', number: 5 },
      { repo: 'a/x', number: 2 },
    ])
    expect(a.get('a/x#m2')).toBe(PALETTE[0])
    expect(a.get('a/x#m5')).toBe(PALETTE[1])
    expect(a.get('b/y#m1')).toBe(PALETTE[2])
  })

  it('repeats the palette from the ninth milestone', () => {
    const ms = Array.from({ length: 9 }, (_, i) => ({ repo: 'a/x', number: i + 1 }))
    const c = assignColors(ms)
    expect(c.get('a/x#m9')).toBe(c.get('a/x#m1'))
    expect(c.get('a/x#m8')).not.toBe(c.get('a/x#m1'))
  })
})
