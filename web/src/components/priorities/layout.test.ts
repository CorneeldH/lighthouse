import { describe, expect, it } from 'vitest'
import type { GNode } from '@/lib/priorities/derive'
import { type DepsOrder, edgeSegments, layoutGraph } from './layout'

const node = (nr: number, depth: number, extra: Partial<GNode> = {}): GNode => ({
  key: `r#${nr}`,
  repo: 'r',
  number: nr,
  title: `t${nr}`,
  url: `u${nr}`,
  milestone: null,
  external: false,
  active: false,
  state: 'ready',
  needsHuman: false,
  uphill: false,
  depth,
  waiting: 0,
  weight: 0,
  factor: 0,
  prioWeight: 0,
  cycle: false,
  component: nr,
  ...extra,
})
const edge = (a: number, b: number) => ({ from: `r#${a}`, to: `r#${b}` })
const at = (res: ReturnType<typeof layoutGraph>, nr: number) => {
  const n = res.nodes.find((x) => x.key === `r#${nr}`)
  if (!n) throw new Error(`missing ${nr}`)
  return n
}

describe('layoutGraph', () => {
  it('puts a chain A->B->C in strictly rising x', () => {
    const res = layoutGraph([node(1, 0), node(2, 1), node(3, 2)], [edge(1, 2), edge(2, 3)])
    expect(at(res, 1).x).toBeLessThan(at(res, 2).x)
    expect(at(res, 2).x).toBeLessThan(at(res, 3).x)
  })

  it('terminates on a cycle A<->B with finite coordinates', () => {
    const res = layoutGraph(
      [node(1, 0, { cycle: true }), node(2, 0, { cycle: true })],
      [edge(1, 2), edge(2, 1)],
    )
    const nums = [res.width, res.height, ...res.nodes.flatMap((n) => [n.x, n.y])]
    for (const edgeOut of res.edges) for (const p of edgeOut.points) nums.push(p.x, p.y)
    expect(nums.every(Number.isFinite)).toBe(true)
    expect(res.edges).toHaveLength(2)
  })

  it('never overlaps two nodes in a diamond of four, also with a stray extra root', () => {
    const res = layoutGraph(
      [node(1, 0), node(5, 0), node(2, 1), node(3, 1), node(4, 2)],
      [edge(1, 2), edge(1, 3), edge(2, 4), edge(3, 4), edge(5, 4)],
    )
    for (const a of res.nodes) {
      for (const b of res.nodes) {
        if (a === b) continue
        const apart = a.x + a.w <= b.x || b.x + b.w <= a.x || a.y + a.h <= b.y || b.y + b.h <= a.y
        expect(apart, `${a.key} vs ${b.key}`).toBe(true)
      }
    }
  })

  it('gives every edge a start at the blocker and an end at the blocked node', () => {
    const res = layoutGraph([node(1, 0), node(2, 1)], [edge(1, 2)])
    const [e] = res.edges
    expect(e.points[0].x).toBe(at(res, 1).x + 210)
    expect(e.points[e.points.length - 1].x).toBe(at(res, 2).x)
  })

  it('routes an edge that skips a column clear of every node in the columns it crosses', () => {
    // 1 (col 0) -> 4 (col 3); columns 1 and 2 hold nodes on the straight line between them.
    const ns = [node(1, 0), node(2, 1), node(3, 1), node(5, 2), node(6, 2), node(4, 3)]
    const es = [edge(1, 2), edge(1, 3), edge(2, 5), edge(3, 6), edge(5, 4), edge(6, 4), edge(1, 4)]
    const res = layoutGraph(ns, es)
    const long = res.edges.find((e) => e.from === 'r#1' && e.to === 'r#4')
    if (!long) throw new Error('missing long edge')
    const middle = res.nodes.filter((n) => ['r#2', 'r#3', 'r#5', 'r#6'].includes(n.key))
    for (const [p0, c1, c2, p3] of edgeSegments(long.points)) {
      for (let i = 0; i <= 200; i++) {
        const t = i / 200
        const m = 1 - t
        const x = m * m * m * p0.x + 3 * m * m * t * c1.x + 3 * m * t * t * c2.x + t * t * t * p3.x
        const y = m * m * m * p0.y + 3 * m * m * t * c1.y + 3 * m * t * t * c2.y + t * t * t * p3.y
        for (const n of middle) {
          const inside = x > n.x && x < n.x + n.w && y > n.y && y < n.y + n.h
          expect(inside, `edge crosses ${n.key} at ${x.toFixed(0)},${y.toFixed(0)}`).toBe(false)
        }
      }
    }
    expect(res.height).toBeGreaterThanOrEqual(Math.max(...long.points.map((p) => p.y)))
  })
})

describe('layoutGraph sorteermodus', () => {
  // Eén kolom (1-4) met een tak naar 5, zodat dagre iets te ordenen heeft; volgorde van boven naar onder.
  const edges = [1, 2, 3, 4].map((nr) => edge(nr, 5))
  const order = (nodes: GNode[], mode?: DepsOrder) => {
    const res = layoutGraph(nodes, edges, mode)
    return nodes
      .filter((n) => n.depth === 0)
      .map((n) => ({ nr: n.number, y: at(res, n.number).y }))
      .sort((a, b) => a.y - b.y)
      .map((n) => n.nr)
  }
  const col = (extra: (nr: number) => Partial<GNode>) => [
    ...[1, 2, 3, 4].map((nr) => node(nr, 0, extra(nr))),
    node(5, 1),
  ]
  // De dagre-volgorde van de kolom, als [eerste, tweede, derde, vierde].
  const dagreOrder = () => order(col(() => ({})), 'crossings')

  it('prio: kolom staat op prioWeight aflopend, dan factor aflopend, dan dagre', () => {
    const [a, b, c, d] = dagreOrder()
    // a: laagste prioWeight maar eerst in dagre; b, c, d: gelijke prioWeight, factor tegen dagre in.
    const pw: Record<number, number> = { [a]: 1, [b]: 5, [c]: 5, [d]: 5 }
    const f: Record<number, number> = { [a]: 9, [b]: 1, [c]: 2, [d]: 3 }
    const nodes = col((nr) => ({ prioWeight: pw[nr], factor: f[nr] }))
    expect(order(nodes, 'prio')).toEqual([d, c, b, a])
    expect(order(nodes, 'prio')).not.toEqual(order(nodes, 'crossings'))
  })

  it('weight: kolom staat op weight aflopend, bij gelijk gewicht in dagre-volgorde', () => {
    const [a, b, c, d] = dagreOrder()
    const w: Record<number, number> = { [a]: 2, [b]: 2, [c]: 7, [d]: 2 }
    // prioWeight en factor wijzen de andere kant op: alleen weight mag tellen.
    const nodes = col((nr) => ({ weight: w[nr], prioWeight: nr, factor: nr }))
    expect(order(nodes, 'weight')).toEqual([c, a, b, d])
  })

  it('crossings: de volgorde is die van dagre, ongeacht weight en prioWeight', () => {
    const plain = dagreOrder()
    const heavy = col((nr) => ({ weight: (nr * 3) % 5, prioWeight: (nr * 7) % 4, factor: nr }))
    expect(order(heavy, 'crossings')).toEqual(plain)
    expect(order(heavy)).toEqual(plain)
  })

  it('prio: gelijke prioWeight én factor valt terug op dagre-volgorde', () => {
    const [a, b, c, d] = dagreOrder()
    // a is eerst in dagre maar het laagst; b, c, d zijn gelijk en houden hun dagre-volgorde.
    const nodes = col((nr) => (nr === a ? { prioWeight: 1, factor: 2 } : { prioWeight: 4, factor: 2 }))
    expect(order(nodes, 'prio')).toEqual([b, c, d, a])
  })

  it('prio: een knoop zonder gewichten (undefined) crasht niet en telt als 0', () => {
    const [a, b, c, d] = dagreOrder()
    const none = { weight: undefined, factor: undefined, prioWeight: undefined } as unknown as Partial<GNode>
    const nodes = col((nr) => (nr === a ? none : { prioWeight: 1 }))
    expect(order(nodes, 'prio')).toEqual([b, c, d, a])
  })
})
