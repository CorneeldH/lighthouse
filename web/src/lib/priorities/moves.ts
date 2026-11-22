import type { MilestoneRow } from './derive'

// Structurally the same as the generated RankChange in api/lighthouse.ts.
export type RankChange = { number: number; from: number | null; to: number }

// Plans the rank changes for moving one milestone a step up or down within its repo, or null when
// the move is impossible. Ranked milestones are first renumbered to 1..k in their current order
// (ties and gaps), then the moved one swaps places with its neighbour. Only changed ranks are
// returned; the moved milestone comes first.
export function planMove(
  rows: MilestoneRow[],
  repo: string,
  number: number,
  dir: 'up' | 'down',
): RankChange[] | null {
  const own = rows.filter((r) => r.repo === repo)
  const row = own.find((r) => r.number === number)
  if (row === undefined || own.length < 2) return null
  // Array.prototype.sort is stable, so equal ranks keep the input order.
  const ranked = own.filter((r) => r.rank !== null).sort((a, b) => (a.rank as number) - (b.rank as number))
  const k = ranked.length
  const place = new Map<number, number>(ranked.map((r, i) => [r.number, i + 1]))

  const current = place.get(number) ?? null
  if (current === null) {
    if (dir === 'down') return null
    place.set(number, k + 1)
  } else {
    const target = dir === 'up' ? current - 1 : current + 1
    if (target < 1 || target > k) return null
    const neighbour = ranked[target - 1]
    place.set(number, target)
    place.set(neighbour.number, current)
  }

  const changes: RankChange[] = []
  for (const r of [row, ...ranked.filter((x) => x !== row)]) {
    const to = place.get(r.number) as number
    if (r.rank !== to) changes.push({ number: r.number, from: r.rank, to })
  }
  return changes
}

export function canMove(rows: MilestoneRow[], number: number, dir: 'up' | 'down'): boolean {
  const row = rows.find((r) => r.number === number)
  return row !== undefined && planMove(rows, row.repo, number, dir) !== null
}
