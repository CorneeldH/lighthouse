import { describe, expect, it } from 'vitest'
import { parseRank } from './rank'

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
