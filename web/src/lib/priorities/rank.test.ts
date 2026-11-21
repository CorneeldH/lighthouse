import { describe, expect, it } from 'vitest'
import { parseRank, setRank } from './rank'

describe('parseRank', () => {
  const cases: Array<[string, string, number | null]> = [
    ['empty', '', null],
    ['plain', 'Priority: 3', 3],
    ['lowercase and spaces', '  priority:7  ', 7],
    ['uppercase, wide gap', 'PRIORITY:  12', 12],
    ['word', 'Priority: high', null],
    ['zero', 'Priority: 0', null],
    ['leading zero', 'Priority: 03', null],
    ['trailing text', 'Priority: 2 extra', null],
    ['list item', '- Priority: 2', null],
    ['first wins', 'a\nPriority: 2\nPriority: 5', 2],
    ['invalid line skipped', 'Priority: high\nPriority: 4', 4],
    ['crlf', 'a\r\nPriority: 6\r\nb', 6],
    ['overflow', 'Priority: 99999999999999999999', null],
    ['largest safe integer', 'Priority: 9007199254740991', 9007199254740991],
    ['one past the safe integer', 'Priority: 9007199254740992', null],
  ]
  it.each(cases)('%s', (_name, input, want) => {
    expect(parseRank(input)).toBe(want)
  })
})

// Mirror of TestSetRank in internal/priorities/rank_test.go.
describe('setRank', () => {
  const cases: Array<[string, string, number, string]> = [
    ['empty', '', 1, 'Priority: 1'],
    ['append', 'Pitch', 2, 'Pitch\n\nPriority: 2'],
    ['replace', 'Pitch\nPriority: 2\nrest', 1, 'Pitch\nPriority: 1\nrest'],
    ['crlf', 'a\r\nPriority: 2\r\nb', 9, 'a\r\nPriority: 9\r\nb'],
    ['first valid line', 'Priority: high\nPriority: 4', 1, 'Priority: high\nPriority: 1'],
    ['keeps indent and trailing', '  Priority: 2  \n', 1, '  Priority: 1  \n'],
    ['trailing newline appended literally', 'Pitch\n', 2, 'Pitch\n\n\nPriority: 2'],
    ['blank description', '  ', 1, '  \n\nPriority: 1'],
    ['trailing crlf appended literally', 'Pitch\r\n', 2, 'Pitch\r\n\n\nPriority: 2'],
    ['overflow is plain text', 'Priority: 99999999999999999999', 3, 'Priority: 99999999999999999999\n\nPriority: 3'],
  ]
  it.each(cases)('%s', (_name, input, rank, want) => {
    expect(setRank(input, rank)).toBe(want)
  })
})
