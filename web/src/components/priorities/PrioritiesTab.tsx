import { ListOrdered } from 'lucide-react'
import { type JSX, useMemo, useState } from 'react'
import { Panel } from '@/components/Panel'
import { usePriorities } from '@/lib/queries'
import { buildGraph, milestoneRows, withoutMilestone } from '@/lib/priorities/derive'
import { assignColors } from '@/lib/priorities/palette'
import { loadPrefs, savePrefs, type ViewId } from '@/lib/priorities/prefs'
import { DepsView } from './DepsView'
import { MilestonesView } from './MilestonesView'
import { Toolbar, type LegendItem } from './Toolbar'
import { UnassignedView } from './UnassignedView'

export function PrioritiesTab(): JSX.Element {
  const { data, error, isPending } = usePriorities()
  const [prefs, setPrefs] = useState(loadPrefs)
  const repos = useMemo(() => data?.repos ?? [], [data])
  const slugs = useMemo(() => repos.map((r) => r.slug), [repos])
  // null = every repo on; a stored slug that no longer exists is ignored.
  const selected = useMemo(
    () => new Set(prefs.repos === null ? slugs : slugs.filter((s) => prefs.repos?.includes(s))),
    [prefs.repos, slugs],
  )
  const graph = useMemo(() => buildGraph(repos, data?.runs ?? []), [repos, data])
  const derived = useMemo(() => {
    const inView = repos.filter((r) => selected.has(r.slug))
    const rows = milestoneRows(inView, graph)
    return {
      counts: {
        deps: [...graph.nodes.values()].filter((n) => selected.has(n.repo)).length,
        milestones: rows.length,
        unassigned: withoutMilestone(inView, graph).filter((r) => selected.has(r.node.repo)).length,
      } satisfies Record<ViewId, number>,
      milestones: rows,
    }
  }, [repos, graph, selected])
  const colors = useMemo(
    () => assignColors(repos.flatMap((r) => r.milestones.map((m) => ({ repo: r.slug, number: m.number })))),
    [repos],
  )

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

  const prefix = selected.size > 1
  const legend: LegendItem[] = derived.milestones.map((m) => ({
    key: `${m.repo}#m${m.number}`,
    label: prefix ? `${m.repo} · ${m.title}` : m.title,
    color: colors.get(`${m.repo}#m${m.number}`) ?? 'var(--lh-grey)',
  }))

  const props = { graph, repos, selected, colors }
  const empty = data !== undefined && repos.length === 0
  const View = prefs.view === 'deps' ? DepsView : prefs.view === 'milestones' ? MilestonesView : UnassignedView
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
            />
            {selected.size === 0 ? <p>No repo selected.</p> : <View {...props} />}
          </>
        )}
      </Panel>
    </div>
  )
}
