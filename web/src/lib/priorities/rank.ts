// A trimmed description line that carries the rank of a milestone.
const RANK_LINE = /^priority:\s*([1-9][0-9]*)$/i

// Returns the rank of the first valid `Priority: N` line, or null. A digit run beyond the safe
// integer range is plain text, like the overflow case in the Go ParseRank.
export function parseRank(description: string): number | null {
  for (const line of description.split('\n')) {
    const m = RANK_LINE.exec(line.trim())
    if (m === null) continue
    const n = Number(m[1])
    if (Number.isSafeInteger(n)) return n
  }
  return null
}
