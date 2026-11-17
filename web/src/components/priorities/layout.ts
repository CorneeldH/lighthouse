import dagre from '@dagrejs/dagre'
import type { GNode, NodeKey } from '@/lib/priorities/derive'

export const NODE_W = 210
export const NODE_H = 38
export const COL_GAP = 80
export const ROW_GAP = 10
// Room above the first row for the column headings.
export const TOP = 26

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
export function layoutGraph(nodes: GNode[], edges: Array<{ from: NodeKey; to: NodeKey }>): LaidGraph {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', nodesep: ROW_GAP, ranksep: COL_GAP })
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
    col.sort((a, b) => (g.node(a.key)?.y ?? 0) - (g.node(b.key)?.y ?? 0) || a.number - b.number)
    col.forEach((n, i) => {
      const y = TOP + i * (NODE_H + ROW_GAP)
      laid.set(n.key, { key: n.key, x: depth * (NODE_W + COL_GAP), y, w: NODE_W, h: NODE_H })
      height = Math.max(height, y + NODE_H)
    })
  }
  const cols = nodes.length === 0 ? 0 : Math.max(...nodes.map((n) => n.depth)) + 1
  const out: LaidEdge[] = []
  for (const e of edges) {
    const a = laid.get(e.from)
    const b = laid.get(e.to)
    if (!a || !b) continue
    const sameColumn = a.x === b.x
    // Within a column (a cycle) the arrow leaves at the bottom and enters at the top.
    const points = sameColumn
      ? [
          { x: a.x + NODE_W / 2, y: a.y + (b.y > a.y ? NODE_H : 0) },
          { x: b.x + NODE_W / 2, y: b.y + (b.y > a.y ? 0 : NODE_H) },
        ]
      : [
          { x: a.x + NODE_W, y: a.y + NODE_H / 2 },
          { x: b.x, y: b.y + NODE_H / 2 },
        ]
    out.push({ from: e.from, to: e.to, points })
  }
  return {
    nodes: [...laid.values()],
    edges: out,
    width: cols === 0 ? 0 : cols * NODE_W + (cols - 1) * COL_GAP,
    height,
  }
}
