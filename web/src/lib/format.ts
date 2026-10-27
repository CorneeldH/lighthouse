export function formatAge(ageS: number): string {
  if (ageS < 60) return '< 1 min'
  if (ageS < 3600) return `${Math.floor(ageS / 60)} min`
  return `${Math.floor(ageS / 3600)} h`
}

function hhmm(d: Date): string {
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export function formatReset(resetAt: string, now: Date): string {
  if (resetAt === '') return ''
  const d = new Date(resetAt)
  if (d.toDateString() === now.toDateString()) return hhmm(d)
  return `${d.toLocaleDateString('en-US', { weekday: 'short' })} ${hhmm(d)}`
}

export function formatPct(pct: number, lowerBound: boolean): string {
  return `${lowerBound ? '≥ ' : ''}${pct}%`
}
