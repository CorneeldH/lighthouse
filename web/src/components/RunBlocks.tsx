import { Hourglass, Moon, Play, PauseCircle } from 'lucide-react'
import type { JSX } from 'react'
import type { DeferredView } from '@/api/lighthouse'
import { ApiError } from '@/lib/api'
import { splitDeferred } from '@/lib/deferred'
import { useRuns } from '@/lib/queries'
import { IssueLink } from './IssueLink'
import { Panel } from './Panel'
import { RunRow } from './RunRow'

function runsErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    return `Runs unavailable: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Server unreachable'
}

const shortErrorText = (): string => 'Runs unavailable'

function RunsBlock({
  id,
  title,
  state,
  icon,
  tone,
  empty,
  note,
  primary = false,
}: {
  id: string
  title: string
  state: 'open' | 'parked'
  icon: typeof Play
  tone: 'blue' | 'grey'
  empty: string
  note?: string
  primary?: boolean
}): JSX.Element {
  const { data, error, isPending } = useRuns()
  const runs = data?.runs.filter((r) => r.state === state)
  return (
    <Panel
      id={id}
      title={title}
      icon={icon}
      tone={tone}
      count={runs?.length}
      error={error}
      errorText={primary ? runsErrorText : shortErrorText}
      loading={isPending && !error}
    >
      {note && <p className="text-muted-foreground">{note}</p>}
      {runs && runs.length === 0 && <p>{empty}</p>}
      {runs && runs.length > 0 && (
        <ul className="space-y-4">
          {runs.map((r) => (
            <RunRow key={`${r.repo}#${r.issue}`} run={r} workspaceUrl={data?.workspaceUrl ?? ''} />
          ))}
        </ul>
      )}
    </Panel>
  )
}

export function RunningBlock(): JSX.Element {
  return <RunsBlock id="running" title="Running runs" state="open" icon={Play} tone="blue" empty="No running runs" primary />
}

export function ParkedBlock(): JSX.Element {
  return (
    <RunsBlock
      id="parked"
      title="Parked runs"
      state="parked"
      icon={PauseCircle}
      tone="grey"
      empty="No parked runs"
      note="Stopped mid-run by the usage brake or lighthouse pause. Resumes by itself after the reset or the pause."
    />
  )
}

function DeferredBlock({
  id,
  title,
  icon,
  tone,
  pick,
  empty,
  note,
  badges,
}: {
  id: string
  title: string
  icon: typeof Play
  tone: 'yellow' | 'navy'
  pick: (d: { queue: DeferredView[]; night: DeferredView[] }) => DeferredView[]
  empty: string
  note: (night: string) => string
  badges: boolean
}): JSX.Element {
  const { data, error, isPending } = useRuns()
  const items = data ? pick(splitDeferred(data.deferred)) : undefined
  return (
    <Panel
      id={id}
      title={title}
      icon={icon}
      tone={tone}
      count={items?.length}
      error={error}
      errorText={shortErrorText}
      loading={isPending && !error}
    >
      {data && <p className="text-muted-foreground">{note(data.night)}</p>}
      {items && items.length === 0 && <p>{empty}</p>}
      {items && items.length > 0 && (
        <ul className="space-y-4">
          {items.map((d) => (
            <li key={`${d.repo}#${d.issue}`} className="space-y-1">
              <p>
                <IssueLink repo={d.repo} issue={d.issue} /> <span>{d.title}</span>
              </p>
              {badges && (
                <p>
                  <span className="rounded-full bg-lh-yellow px-2 py-0.5 text-xs font-medium text-[#14213d]">{d.reason}</span>
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}

export function QueueBlock(): JSX.Element {
  return (
    <DeferredBlock
      id="queue"
      title="Queue"
      icon={Hourglass}
      tone="yellow"
      pick={(s) => s.queue}
      empty="Nothing in the queue"
      note={() => 'Ready to start, held by a brake. The next tick with room starts it.'}
      badges
    />
  )
}

export function NightBlock(): JSX.Element {
  return (
    <DeferredBlock
      id="night"
      title="Night"
      icon={Moon}
      tone="navy"
      pick={(s) => s.night}
      empty="Nothing waiting for the night"
      note={(night) => {
        const begin = night.split('-')[0] ?? ''
        return begin === '' ? 'Handed off for the night window.' : `Handed off for the night window. Starts from ${begin}.`
      }}
      badges={false}
    />
  )
}
