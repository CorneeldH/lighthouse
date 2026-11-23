import dagre from '@dagrejs/dagre'
import type { GNode, NodeKey } from '@/lib/priorities/derive'

export const NODE_W = 210
export const NODE_H = 38
export const COL_GAP = 80
export const ROW_GAP = 10
// Room above the first row for the column headings.
export const TOP = 26
const LANE = 10

export interface LaidNode {
  key: NodeKey
  x: number
  y: number
  w: 210
  h: 38
}

export interface LaidEdge {
  from: NodeKey
  to: NodeKey
  points: Array<{ x: number; y: number }>
}

export interface LaidGraph {
  nodes: LaidNode[]
  edges: LaidEdge[]
  width: number
  height: number
}

// The column is the chain depth (GNode.depth), so "Can start now" is always the first column.
// dagre (rankdir LR) only supplies the order inside a column, which keeps crossings low; the rows
// are then packed per column, so two nodes never overlap whatever rank dagre gave them.
export type DepsOrder = 'prio' | 'weight' | 'crossings'

export function layoutGraph(
  nodes: GNode[],
  edges: Array<{ from: NodeKey; to: NodeKey }>,
  order: DepsOrder = 'crossings',
): LaidGraph {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', nodesep: ROW_GAP, ranksep: COL_GAP, ranker: 'longest-path' })
  g.setDefaultEdgeLabel(() => ({}))
  const known = new Set(nodes.map((n) => n.key))
  for (const n of nodes) g.setNode(n.key, { width: NODE_W, height: NODE_H })
  for (const e of edges) {
    if (e.from !== e.to && known.has(e.from) && known.has(e.to)) g.setEdge(e.from, e.to)
  }
  dagre.layout(g)

  const byDepth = new Map<number, GNode[]>()
  for (const n of nodes) byDepth.set(n.depth, [...(byDepth.get(n.depth) ?? []), n])
  const laid = new Map<NodeKey, LaidNode>()
  let height = 0
  for (const [depth, col] of byDepth) {
    const byDagre = (a: GNode, b: GNode) =>
      (g.node(a.key)?.y ?? 0) - (g.node(b.key)?.y ?? 0) || a.number - b.number
    const key = (a: GNode, b: GNode) =>
      order === 'prio'
        ? (b.prioWeight ?? 0) - (a.prioWeight ?? 0) || (b.factor ?? 0) - (a.factor ?? 0)
        : order === 'weight'
          ? (b.weight ?? 0) - (a.weight ?? 0)
          : 0
    col.sort((a, b) => key(a, b) || byDagre(a, b))
    col.forEach((n, i) => {
      const y = TOP + i * (NODE_H + ROW_GAP)
      laid.set(n.key, { key: n.key, x: depth * (NODE_W + COL_GAP), y, w: NODE_W, h: NODE_H })
      height = Math.max(height, y + NODE_H)
    })
  }
  const cols = nodes.length === 0 ? 0 : Math.max(...nodes.map((n) => n.depth)) + 1
  // Lowest edge of each column: a long edge runs through a lane just below it.
  const colBottom = new Map<number, number>()
  for (const n of nodes) {
    const p = laid.get(n.key) as LaidNode
    colBottom.set(n.depth, Math.max(colBottom.get(n.depth) ?? TOP, p.y + NODE_H))
  }
  const depthOf = new Map(nodes.map((n) => [n.key, n.depth]))
  const out: LaidEdge[] = []
  for (const e of edges) {
    const a = laid.get(e.from)
    const b = laid.get(e.to)
    if (!a || !b) continue
    const sameColumn = a.x === b.x
    // Within a column (a cycle) the arrow leaves at the bottom and enters at the top.
    let points = sameColumn
      ? [
          { x: a.x + NODE_W / 2, y: a.y + (b.y > a.y ? NODE_H : 0) },
          { x: b.x + NODE_W / 2, y: b.y + (b.y > a.y ? 0 : NODE_H) },
        ]
      : [
          { x: a.x + NODE_W, y: a.y + NODE_H / 2 },
          { x: b.x, y: b.y + NODE_H / 2 },
        ]
    const from = depthOf.get(e.from) as number
    const to = depthOf.get(e.to) as number
    if (to - from > 1) {
      // Skipped columns: pass each through the lane under its last node, entering and leaving
      // through the gaps between columns, where no node ever stands.
      const mid: Array<{ x: number; y: number }> = []
      for (let c = from + 1; c < to; c++) {
        const lane = (colBottom.get(c) ?? TOP) + LANE
        const x = c * (NODE_W + COL_GAP)
        mid.push({ x: x - COL_GAP / 4, y: lane }, { x: x + NODE_W + COL_GAP / 4, y: lane })
      }
      points = [points[0], ...mid, points[1]]
    }
    height = Math.max(height, ...points.map((p) => p.y + 4))
    out.push({ from: e.from, to: e.to, points })
  }
  return {
    nodes: [...laid.values()],
    edges: out,
    width: cols === 0 ? 0 : cols * NODE_W + (cols - 1) * COL_GAP,
    height,
  }
}

type Pt = { x: number; y: number }
// Cubic segments of an edge: one S-curve per consecutive pair of points, with horizontal tangents.
// A same-column edge (cycle) bulges out to the right instead.
export function edgeSegments(points: Pt[]): Array<[Pt, Pt, Pt, Pt]> {
  const segs: Array<[Pt, Pt, Pt, Pt]> = []
  for (let i = 0; i + 1 < points.length; i++) {
    const a = points[i]
    const b = points[i + 1]
    if (a.x === b.x) {
      const dx = NODE_W / 2 + 20
      segs.push([a, { x: a.x + dx, y: a.y }, { x: b.x + dx, y: b.y }, b])
    } else {
      const mx = (a.x + b.x) / 2
      segs.push([a, { x: mx, y: a.y }, { x: mx, y: b.y }, b])
    }
  }
  return segs
}

export function edgePath(points: Pt[]): string {
  const segs = edgeSegments(points)
  return segs.reduce(
    (d, [, c1, c2, p], i) =>
      `${i === 0 ? `M${segs[0][0].x},${segs[0][0].y} ` : `${d} `}C${c1.x},${c1.y} ${c2.x},${c2.y} ${p.x},${p.y}`,
    '',
  )
}
