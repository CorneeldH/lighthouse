// A trimmed description line that carries the rank of a milestone.
const RANK_LINE = /^priority:\s*([1-9][0-9]*)$/i

// Returns the rank of the first valid `Priority: N` line, or null. A digit run beyond the safe
// integer range is plain text, like the overflow case in the Go ParseRank.
export function parseRank(description: string): number | null {
  const line = description.split('\n').find(isRankLine)
  return line === undefined ? null : Number(RANK_LINE.exec(line.trim())?.[1])
}

function isRankLine(line: string): boolean {
  const m = RANK_LINE.exec(line.trim())
  return m !== null && Number.isSafeInteger(Number(m[1]))
}

// Mirror of the Go SetRank: replaces the first valid Priority line with rank, keeping its
// indentation and trailing whitespace (a `\r` included), or appends a new line after a blank
// line when there is none.
export function setRank(description: string, rank: number): string {
  const newLine = `Priority: ${rank}`
  const lines = description.split('\n')
  const i = lines.findIndex(isRankLine)
  if (i >= 0) {
    const line = lines[i]
    const start = line.length - line.trimStart().length
    const end = line.trimEnd().length
    lines[i] = line.slice(0, start) + newLine + line.slice(end)
    return lines.join('\n')
  }
  if (description === '') return newLine
  return `${description}\n\n${newLine}`
}
