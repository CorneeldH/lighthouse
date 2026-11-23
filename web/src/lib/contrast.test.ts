import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// The ?raw import comes back empty under the tailwind plugin in vitest, so read the file itself (vitest runs from web/).
const css = readFileSync(resolve(process.cwd(), 'src/index.css'), 'utf8')

function luminance(hex: string): number {
  const n = parseInt(hex.replace('#', ''), 16)
  const ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
    const c = v / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2]
}

// WCAG contrast ratio of two #rrggbb colours.
export function contrastRatio(fg: string, bg: string): number {
  const [a, b] = [luminance(fg), luminance(bg)].sort((x, y) => y - x)
  return (a + 0.05) / (b + 0.05)
}

function token(block: string, name: string): string {
  const m = block.match(new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6})`))
  if (!m) throw new Error(`${name} not found`)
  return m[1]
}

describe('chip contrast', () => {
  it('has a readable css source', () => {
    expect(css.length).toBeGreaterThan(0)
  })

  it('computes known ratios', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5)
  })

  it('is at least 4.5 in light and dark', () => {
    const darkStart = css.indexOf('@media (prefers-color-scheme: dark) {')
    expect(darkStart).toBeGreaterThan(0)
    const light = css.slice(0, darkStart)
    const dark = css.slice(darkStart)
    for (const block of [light, dark]) {
      expect(contrastRatio(token(block, '--lh-chip-fg'), token(block, '--lh-chip-bg'))).toBeGreaterThanOrEqual(4.5)
    }
  })
})
