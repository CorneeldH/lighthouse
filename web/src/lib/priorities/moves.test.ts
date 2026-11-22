import { describe, expect, it } from 'vitest'
import { canMove, planMove } from './moves'
import type { MilestoneRow } from './derive'

const repo = 'o/r'

// rows are one repo's milestones in priority order: ranked first, then unranked.
function rows(...ranks: Array<number | null>): MilestoneRow[] {
  return ranks.map((rank, i) => ({ repo, number: i + 1, rank }) as MilestoneRow)
}

describe('planMove', () => {
  it('up swaps ranks with the milestone above', () => {
    expect(planMove(rows(1, 2, 3), repo, 3, 'up')).toEqual([
      { number: 3, from: 3, to: 2 },
      { number: 2, from: 2, to: 3 },
    ])
  })

  it('down swaps ranks with the milestone below', () => {
    expect(planMove(rows(1, 2, 3), repo, 1, 'down')).toEqual([
      { number: 1, from: 1, to: 2 },
      { number: 2, from: 2, to: 1 },
    ])
  })

  it('up on place 1 is impossible', () => {
    expect(planMove(rows(1, 2), repo, 1, 'up')).toBeNull()
  })

  it('down on the last ranked milestone is impossible', () => {
    expect(planMove(rows(1, 2, null), repo, 2, 'down')).toBeNull()
  })

  it('up on an unranked milestone takes the highest rank plus one', () => {
    expect(planMove(rows(1, 2, null), repo, 3, 'up')).toEqual([{ number: 3, from: null, to: 3 }])
  })

  it('down on an unranked milestone is impossible', () => {
    expect(planMove(rows(1, null), repo, 2, 'down')).toBeNull()
  })

  it('renumbers gaps and ties first, then swaps; returns only changed ranks', () => {
    // ranks 1,1,4 -> places 1,2,3; moving #3 up swaps places 3 and 2.
    expect(planMove(rows(1, 1, 4), repo, 3, 'up')).toEqual([
      { number: 3, from: 4, to: 2 },
      { number: 2, from: 1, to: 3 },
    ])
  })

  it('renumbering writes more than two milestones on the first click', () => {
    // places 1,2,3; #2 swaps with #1 (rank 2 -> place 2, unchanged); #3 only renumbers.
    expect(planMove(rows(2, 5, 9), repo, 2, 'up')).toEqual([
      { number: 2, from: 5, to: 1 },
      { number: 3, from: 9, to: 3 },
    ])
  })

  it('a repo with one milestone cannot move', () => {
    expect(planMove(rows(1), repo, 1, 'up')).toBeNull()
    expect(planMove(rows(1), repo, 1, 'down')).toBeNull()
    expect(planMove(rows(null), repo, 1, 'up')).toBeNull()
  })

  it('an unknown milestone cannot move', () => {
    expect(planMove(rows(1, 2), repo, 99, 'up')).toBeNull()
  })
})

describe('canMove', () => {
  it('mirrors planMove', () => {
    const r = rows(1, 2, null)
    expect(canMove(r, 1, 'up')).toBe(false)
    expect(canMove(r, 1, 'down')).toBe(true)
    expect(canMove(r, 2, 'down')).toBe(false)
    expect(canMove(r, 3, 'up')).toBe(true)
    expect(canMove(r, 3, 'down')).toBe(false)
  })
})
