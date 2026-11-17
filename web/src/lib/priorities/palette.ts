// Milestone palette: eight colours derived from --lh-blue, --lh-purple, --lh-green and --lh-orange
// (pure, then a lighter mix). Yellow, grey and navy stay reserved for status.
// Validation: niet gevalideerd — the dataviz skill validator is not available in this environment;
// the PR notes this.
export const PALETTE: readonly string[] = [
  'var(--lh-blue)',
  'var(--lh-purple)',
  'var(--lh-green)',
  'var(--lh-orange)',
  'color-mix(in srgb, var(--lh-blue) 55%, white)',
  'color-mix(in srgb, var(--lh-purple) 55%, white)',
  'color-mix(in srgb, var(--lh-green) 55%, white)',
  'color-mix(in srgb, var(--lh-orange) 55%, white)',
]

// "No milestone" is always grey, and never one of the palette colours.
export const NO_MILESTONE_COLOR = 'var(--lh-grey)'

// Colours go by (repo, number) order, not by input order; the palette repeats when exhausted.
// Key: "<repo>#m<nr>".
export function assignColors(
  milestones: Array<{ repo: string; number: number }>,
): Map<string, string> {
  const sorted = [...milestones].sort(
    (a, b) => (a.repo < b.repo ? -1 : a.repo > b.repo ? 1 : 0) || a.number - b.number,
  )
  const out = new Map<string, string>()
  for (const m of sorted) {
    const key = `${m.repo}#m${m.number}`
    if (!out.has(key)) out.set(key, PALETTE[out.size % PALETTE.length])
  }
  return out
}
