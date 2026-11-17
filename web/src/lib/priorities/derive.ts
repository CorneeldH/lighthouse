import type { Issue, MilestoneRef, Repo, Run } from '../../api/lighthouse'

export type NodeKey = string

export interface GNode {
  key: NodeKey
  repo: string
  number: number
  title: string
  url: string
  milestone: MilestoneRef | null
  external: boolean
  active: boolean
  uphill: boolean
  depth: number
  waiting: number
  cycle: boolean
  component: number
}

export interface Graph {
  nodes: Map<NodeKey, GNode>
  // from = blocker, to = blocked issue
  edges: Array<{ from: NodeKey; to: NodeKey }>
}

const UPHILL_PHASES = new Set([
  'brainstorming',
  'start-issue',
  'spec-review',
  'writing-plans',
  'plan-review',
])

const keyOf = (repo: string, nr: number): NodeKey => `${repo}#${nr}`

function liveRun(runs: Run[], repo: string, nr: number): Run | undefined {
  return runs.find(
    (r) => r.repo === repo && r.issue === nr && (r.state === 'open' || r.state === 'parked'),
  )
}

export function isActive(issue: Issue, runs: Run[], repo: string): boolean {
  return liveRun(runs, repo, issue.number) !== undefined || issue.status === 'In progress'
}

export function isUphill(run: Run | undefined): boolean {
  return run !== undefined && UPHILL_PHASES.has(run.phase)
}

// Iterative Tarjan: returns the component index per node index.
function scc(n: number, adj: number[][]): { comp: number[]; count: number } {
  const index = new Array<number>(n).fill(-1)
  const low = new Array<number>(n).fill(0)
  const onStack = new Array<boolean>(n).fill(false)
  const comp = new Array<number>(n).fill(-1)
  const stack: number[] = []
  let counter = 0
  let count = 0
  for (let s = 0; s < n; s++) {
    if (index[s] !== -1) continue
    const work: Array<[number, number]> = [[s, 0]]
    index[s] = low[s] = counter++
    stack.push(s)
    onStack[s] = true
    while (work.length > 0) {
      const frame = work[work.length - 1]
      const v = frame[0]
      if (frame[1] < adj[v].length) {
        const w = adj[v][frame[1]++]
        if (index[w] === -1) {
          index[w] = low[w] = counter++
          stack.push(w)
          onStack[w] = true
          work.push([w, 0])
        } else if (onStack[w]) {
          low[v] = Math.min(low[v], index[w])
        }
      } else {
        work.pop()
        if (work.length > 0) {
          const p = work[work.length - 1][0]
          low[p] = Math.min(low[p], low[v])
        }
        if (low[v] === index[v]) {
          let w: number
          do {
            w = stack.pop() as number
            onStack[w] = false
            comp[w] = count
          } while (w !== v)
          count++
        }
      }
    }
  }
  return { comp, count }
}

export function buildGraph(repos: Repo[], runs: Run[]): Graph {
  const nodes = new Map<NodeKey, GNode>()
  const edges: Graph['edges'] = []
  const blank = (key: NodeKey, repo: string, number: number): GNode => ({
    key,
    repo,
    number,
    title: '',
    url: '',
    milestone: null,
    external: true,
    active: false,
    uphill: false,
    depth: 0,
    waiting: 0,
    cycle: false,
    component: 0,
  })

  for (const repo of repos) {
    for (const issue of repo.issues ?? []) {
      const run = liveRun(runs, repo.slug, issue.number)
      const active = isActive(issue, runs, repo.slug)
      const key = keyOf(repo.slug, issue.number)
      nodes.set(key, {
        ...blank(key, repo.slug, issue.number),
        title: issue.title,
        url: issue.url,
        milestone: issue.milestone ?? null,
        external: false,
        active,
        uphill: active && isUphill(run),
      })
    }
  }
  for (const repo of repos) {
    for (const issue of repo.issues ?? []) {
      const to = keyOf(repo.slug, issue.number)
      for (const b of issue.blocked_by ?? []) {
        const from = keyOf(b.repo, b.issue)
        if (!nodes.has(from)) nodes.set(from, blank(from, b.repo, b.issue))
        edges.push({ from, to })
      }
    }
  }

  const keys = [...nodes.keys()]
  const idx = new Map(keys.map((k, i) => [k, i]))
  const n = keys.length
  const adj: number[][] = keys.map(() => [])
  for (const e of edges) (adj[idx.get(e.from) as number]).push(idx.get(e.to) as number)

  const { comp, count } = scc(n, adj)
  const size = new Array<number>(count).fill(0)
  comp.forEach((c) => size[c]++)
  const selfLoop = new Set<number>()
  for (const e of edges) if (e.from === e.to) selfLoop.add(comp[idx.get(e.from) as number])

  // Condensed DAG
  const cadj: Array<Set<number>> = Array.from({ length: count }, () => new Set<number>())
  const indeg = new Array<number>(count).fill(0)
  for (let v = 0; v < n; v++) {
    for (const w of adj[v]) {
      if (comp[v] !== comp[w] && !cadj[comp[v]].has(comp[w])) {
        cadj[comp[v]].add(comp[w])
        indeg[comp[w]]++
      }
    }
  }
  const depth = new Array<number>(count).fill(0)
  const queue: number[] = []
  indeg.forEach((d, c) => d === 0 && queue.push(c))
  for (let h = 0; h < queue.length; h++) {
    const c = queue[h]
    for (const d of cadj[c]) {
      depth[d] = Math.max(depth[d], depth[c] + 1)
      if (--indeg[d] === 0) queue.push(d)
    }
  }

  // Waiting: distinct nodes in downstream components, own component excluded.
  // O(C*(C+E)), computed once per fetch.
  const members: number[][] = Array.from({ length: count }, () => [])
  comp.forEach((c, v) => members[c].push(v))
  const waiting = new Array<number>(count).fill(0)
  for (let c = 0; c < count; c++) {
    const seen = new Set<number>([c])
    const todo = [...cadj[c]]
    let total = 0
    while (todo.length > 0) {
      const d = todo.pop() as number
      if (seen.has(d)) continue
      seen.add(d)
      total += members[d].length
      todo.push(...cadj[d])
    }
    waiting[c] = total
  }

  keys.forEach((k, i) => {
    const node = nodes.get(k) as GNode
    const c = comp[i]
    node.component = c
    node.depth = depth[c]
    node.waiting = waiting[c]
    node.cycle = size[c] > 1 || selfLoop.has(c)
  })
  return { nodes, edges }
}

export function isShaping(description: string, issueCount: number): boolean {
  if (issueCount === 0) return true
  return description.split(/\r?\n/).some((l) => l.trim().toLowerCase() === 'status: shaping')
}

export interface MilestoneRow {
  repo: string
  number: number
  title: string
  done: number
  active: GNode[]
  todo: GNode[]
  open: number
  total: number
  percent: number | null
  shaping: boolean
  empty: boolean
  outside: GNode[]
  order: GNode[]
}

const byRepoNumber = (a: GNode, b: GNode) =>
  (a.repo < b.repo ? -1 : a.repo > b.repo ? 1 : 0) || a.number - b.number

export function milestoneRows(repos: Repo[], graph: Graph): MilestoneRow[] {
  const incoming = new Map<NodeKey, NodeKey[]>()
  for (const e of graph.edges) {
    const l = incoming.get(e.to) ?? []
    l.push(e.from)
    incoming.set(e.to, l)
  }
  const rows: MilestoneRow[] = []
  for (const repo of repos) {
    for (const m of repo.milestones ?? []) {
      const own = [...graph.nodes.values()].filter(
        (n) => !n.external && n.repo === repo.slug && n.milestone?.number === m.number,
      )
      const ownKeys = new Set(own.map((n) => n.key))
      const seen = new Set<NodeKey>()
      const todo = own.map((n) => n.key)
      while (todo.length > 0) {
        const k = todo.pop() as NodeKey
        for (const f of incoming.get(k) ?? []) {
          if (seen.has(f)) continue
          seen.add(f)
          todo.push(f)
        }
      }
      const outside = [...seen]
        .filter((k) => !ownKeys.has(k))
        .map((k) => graph.nodes.get(k) as GNode)
        .sort(byRepoNumber)
      const compMin = new Map<number, GNode>()
      for (const n of own) {
        const cur = compMin.get(n.component)
        if (!cur || byRepoNumber(n, cur) < 0) compMin.set(n.component, n)
      }
      const order = [...own].sort(
        (a, b) =>
          a.depth - b.depth ||
          byRepoNumber(compMin.get(a.component) as GNode, compMin.get(b.component) as GNode) ||
          byRepoNumber(a, b),
      )
      const open = own.length
      const empty = open === 0 && m.done === 0
      rows.push({
        repo: repo.slug,
        number: m.number,
        title: m.title,
        done: m.done,
        active: order.filter((n) => n.active),
        todo: order.filter((n) => !n.active),
        open,
        total: m.done + open,
        percent: empty ? null : m.done / (m.done + open),
        shaping: isShaping(m.description, m.done + open),
        empty,
        outside,
        order,
      })
    }
  }
  return rows
}

export function sortMilestoneRows(rows: MilestoneRow[]): MilestoneRow[] {
  return [...rows].sort(
    (a, b) =>
      Number(a.empty) - Number(b.empty) ||
      a.open - b.open ||
      (b.percent ?? 0) - (a.percent ?? 0) ||
      (a.repo < b.repo ? -1 : a.repo > b.repo ? 1 : 0) ||
      a.number - b.number,
  )
}

export interface UnassignedRow {
  node: GNode
  closedMilestone: string | null
  relation: 'blocks' | 'waits' | null
  relatedNumber?: number
}

export function withoutMilestone(_repos: Repo[], graph: Graph): UnassignedRow[] {
  const rows: UnassignedRow[] = []
  for (const node of graph.nodes.values()) {
    if (node.external) continue
    const closed = node.milestone !== null && node.milestone.state.toLowerCase() !== 'open'
    if (node.milestone !== null && !closed) continue
    const blocks = graph.edges.filter((e) => e.from === node.key).map((e) => graph.nodes.get(e.to) as GNode)
    const waits = graph.edges.filter((e) => e.to === node.key).map((e) => graph.nodes.get(e.from) as GNode)
    const row: UnassignedRow = {
      node,
      closedMilestone: closed ? (node.milestone as MilestoneRef).title : null,
      relation: blocks.length > 0 ? 'blocks' : waits.length > 0 ? 'waits' : null,
    }
    const rel = blocks.length > 0 ? blocks : waits
    if (rel.length > 0) row.relatedNumber = Math.min(...rel.map((r) => r.number))
    rows.push(row)
  }
  return rows.sort(
    (a, b) =>
      Number(b.relation !== null) - Number(a.relation !== null) ||
      b.node.number - a.node.number ||
      (a.node.repo < b.node.repo ? -1 : 1),
  )
}
