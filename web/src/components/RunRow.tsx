import { Activity, Check, Clock, Info, Minus, Pause } from 'lucide-react'
import { type JSX, useId } from 'react'
import type { RunView } from '@/api/lighthouse'
import { formatClock, formatDuration } from '@/lib/format'
import { useNow } from '@/lib/now'
import { ExternalLink } from './ExternalLink'
import { IssueLink } from './IssueLink'

const DONE_TITLE = 'Claude finished its turn; the next tick decides: handoff, done, question or returned'

const badge = 'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium'

// The tick writes "" when it could not measure the session of a run.
function StandBadge({ stand }: { stand: string }): JSX.Element {
  switch (stand) {
    case 'working':
      return (
        <span className={`${badge} bg-lh-yellow text-[#14213d]`}>
          <Activity aria-hidden className="size-3" />
          working
        </span>
      )
    case 'done':
      return (
        <span className={`${badge} bg-lh-green text-white dark:text-[#0b1020]`} title={DONE_TITLE}>
          <Check aria-hidden className="size-3" />
          done · next tick decides
        </span>
      )
    case 'waiting':
      return (
        <span className={`${badge} bg-lh-orange text-[#14213d] dark:text-[#0b1020]`}>
          <Pause aria-hidden className="size-3" />
          waiting
        </span>
      )
    default:
      return (
        <span className={`${badge} bg-lh-grey text-white dark:text-[#0b1020]`}>
          <Minus aria-hidden className="size-3" />
          not measured
        </span>
      )
  }
}

type Segment = 'done' | 'build' | 'review' | 'final' | 'pending'

const segmentClass: Record<Segment, string> = {
  done: 'bg-lh-blue',
  build: 'bg-lh-yellow',
  review: 'bg-lh-purple',
  final: 'bg-lh-green',
  pending: 'bg-border',
}

function segments(run: RunView): Segment[] {
  const finalReview = run.step === 'final-review'
  return Array.from({ length: run.tasks }, (_, i): Segment => {
    if (finalReview) return 'final'
    const n = i + 1
    if (n < run.task) return 'done'
    if (n === run.task) return run.step === 'review' ? 'review' : 'build'
    return 'pending'
  })
}

function Progress({ run }: { run: RunView }): JSX.Element | null {
  if (run.tasks <= 0) {
    return run.phase === '' ? null : <span>{run.phase}</span>
  }
  const finalReview = run.step === 'final-review'
  const label = finalReview ? 'final review' : `task ${run.task} of ${run.tasks} · ${run.step === 'review' ? 'review' : 'build'}`
  return (
    <span className="inline-flex items-center gap-2" role="img" title={label} aria-label={label}>
      <span>{finalReview ? `${run.tasks}/${run.tasks}` : `${run.task}/${run.tasks}`}</span>
      <span aria-hidden className="inline-flex gap-0.5">
        {segments(run).map((s, i) => (
          <span key={i} className={`h-2 w-3 rounded-sm ${segmentClass[s]}`} />
        ))}
      </span>
    </span>
  )
}

export function RunRow({ run, workspaceUrl }: { run: RunView; workspaceUrl: string }): JSX.Element {
  const now = useNow()
  const tipId = useId()
  const started = formatClock(run.started_at)
  return (
    <li className="space-y-1">
      <p>
        <IssueLink repo={run.repo} issue={run.issue} /> <span>{run.title}</span>
      </p>
      <p className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <StandBadge stand={run.stand} />
        <Progress run={run} />
        <span className="inline-flex items-center gap-1" title={`running since ${started}`}>
          <Clock aria-hidden className="size-3.5" />
          {formatDuration(Math.max(0, (now - Date.parse(run.started_at)) / 1000))}
        </span>
        {run.workspace_id !== '' &&
          (workspaceUrl !== '' ? (
            <ExternalLink
              href={`${workspaceUrl}/workspaces/${encodeURIComponent(run.workspace_id)}`}
              label={`${run.workspace_id}, opens kenn-forge in a new tab`}
            >
              {run.workspace_id}
            </ExternalLink>
          ) : (
            <code>{run.workspace_id}</code>
          ))}
        <span className="group relative inline-flex">
          <button type="button" aria-label="Run details" aria-describedby={tipId} className="rounded-full">
            <Info aria-hidden className="size-4" />
          </button>
          <span
            id={tipId}
            role="tooltip"
            className="invisible absolute left-0 top-full z-10 mt-1 w-max max-w-xs space-y-0.5 rounded-md border bg-card p-2 text-xs shadow-md group-hover:visible group-focus-within:visible"
          >
            <span className="block">
              command <code>{run.command}</code>
            </span>
            <span className="block">trigger {run.trigger}</span>
            <span className="block">profile {run.profile}</span>
            <span className="block">started {started}</span>
          </span>
        </span>
      </p>
    </li>
  )
}
