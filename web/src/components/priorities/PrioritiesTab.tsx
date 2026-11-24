import { ListOrdered } from 'lucide-react'
import { type JSX, useMemo, useState } from 'react'
import { Panel } from '@/components/Panel'
import { useMilestoneRanks, usePriorities } from '@/lib/queries'
import {
  buildGraph,
  milestoneRows,
  priorityOrder,
  sortMilestoneRows,
  withoutMilestone,
  type MilestoneRow,
} from '@/lib/priorities/derive'
import { planMove } from '@/lib/priorities/moves'
import { assignColors, NO_MILESTONE_COLOR } from '@/lib/priorities/palette'
import { loadPrefs, savePrefs, type ViewId } from '@/lib/priorities/prefs'
import { DepsView } from './DepsView'
import { MilestonesView } from './MilestonesView'
import { Toolbar, type LegendItem } from './Toolbar'
import { BacklogView } from './BacklogView'

export function PrioritiesTab(): JSX.Element {
  const { data, error, isPending } = usePriorities()
  const [prefs, setPrefs] = useState(loadPrefs)
  const ranks = useMilestoneRanks()
  const repos = useMemo(() => data?.repos ?? [], [data])
  const slugs = useMemo(() => repos.map((r) => r.slug), [repos])
  // null = every repo on; a stored slug that no longer exists is ignored.
  const selected = useMemo(
    () => {
      const valid = prefs.repos === null ? [] : slugs.filter((s) => prefs.repos?.includes(s))
      return new Set(valid.length === 0 && prefs.repos?.length !== 0 ? slugs : valid)
    },
    [prefs.repos, slugs],
  )
  const graph = useMemo(() => buildGraph(repos, data?.runs ?? []), [repos, data])
  const derived = useMemo(() => {
    const inView = repos.filter((r) => selected.has(r.slug))
    const rows = milestoneRows(inView, graph)
    const unassigned = withoutMilestone(inView, graph).filter((r) => selected.has(r.node.repo))
    const nodes = [...graph.nodes.values()].filter((n) => selected.has(n.repo))
    return {
      // Issues without a milestone in view: nodes in the graph, every row of the unassigned view.
      hasUnassigned: {
        deps: nodes.some((n) => !n.milestone),
        milestones: false,
        backlog: unassigned.length > 0,
      } satisfies Record<ViewId, boolean>,
      counts: {
        deps: nodes.length,
        milestones: rows.length,
        backlog: unassigned.length,
      } satisfies Record<ViewId, number>,
      // Milestones that get a colour, per view: nodes shown in the graph, the rows shown, the
      // closed milestones named on the rows without an open milestone.
      inViewMilestones: {
        deps: nodes.flatMap((n) => (n.milestone ? [{ repo: n.repo, number: n.milestone.number, title: n.milestone.title }] : [])),
        milestones: rows.map((m) => ({ repo: m.repo, number: m.number, title: m.title })),
        backlog: unassigned.flatMap((r) =>
          r.node.milestone ? [{ repo: r.node.repo, number: r.node.milestone.number, title: r.node.milestone.title }] : [],
        ),
      } satisfies Record<ViewId, Array<{ repo: string; number: number; title: string }>>,
    }
  }, [repos, graph, selected])
  const prefix = selected.size > 1
  const shown = derived.inViewMilestones[prefs.view]
  const colors = useMemo(() => assignColors(shown), [shown])

  const update = (next: typeof prefs) => {
    setPrefs(next)
    savePrefs(next)
  }
  const toggleRepo = (slug: string) => {
    const set = new Set(selected)
    if (set.has(slug)) set.delete(slug)
    else set.add(slug)
    update({ ...prefs, repos: slugs.filter((s) => set.has(s)) })
  }

  const legend: LegendItem[] = [...colors.keys()].map((key) => {
    const m = shown.find((x) => `${x.repo}#m${x.number}` === key) as (typeof shown)[number]
    return { key, label: prefix ? `${m.repo} · ${m.title}` : m.title, color: colors.get(key) as string }
  })
  if (derived.hasUnassigned[prefs.view]) legend.push({ key: 'none', label: 'Without milestone', color: NO_MILESTONE_COLOR })

  // planMove needs every milestone of the repo in its current priority order, so the rows are
  // built from the unfiltered repo, independent of what the view shows.
  const onMove = (row: MilestoneRow, dir: 'up' | 'down') => {
    const own = priorityOrder(sortMilestoneRows(milestoneRows(repos.filter((r) => r.slug === row.repo), graph)))
    const changes = planMove(own, row.repo, row.number, dir)
    if (changes !== null && changes.length > 0) ranks.mutate({ repo: row.repo, changes })
  }
  const pendingRepos = new Set(slugs.filter(ranks.isPending))

  const props = { graph, repos, selected, colors, order: prefs.msOrder, depsOrder: prefs.depsOrder, onMove, pendingRepos }
  const empty = data !== undefined && repos.length === 0
  const View = prefs.view === 'deps' ? DepsView : prefs.view === 'milestones' ? MilestonesView : BacklogView
  return (
    <div className="w-full">
      <Panel id="priorities" title="Priorities" icon={ListOrdered} tone="navy" error={error} loading={isPending && !error}>
        {empty && <p>No repos configured.</p>}
        {data !== undefined && !empty && (
          <>
            <Toolbar
              view={prefs.view}
              onView={(view) => update({ ...prefs, view })}
              counts={derived.counts}
              repos={slugs}
              selected={selected}
              onToggleRepo={toggleRepo}
              legend={legend}
              msOrder={prefs.msOrder}
              onMsOrder={(msOrder) => update({ ...prefs, msOrder })}
              depsOrder={prefs.depsOrder}
              onDepsOrder={(depsOrder) => update({ ...prefs, depsOrder })}
            />
            {ranks.error !== null && (
              <p role="alert" className="text-sm font-medium text-destructive">
                {ranks.error}
              </p>
            )}
            {selected.size === 0 ? <p>No repo selected.</p> : <View {...props} />}
          </>
        )}
      </Panel>
    </div>
  )
}
