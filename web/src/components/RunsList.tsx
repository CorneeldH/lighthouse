import { Play } from 'lucide-react'
import { type JSX, useId } from 'react'
import { ApiError } from '@/lib/api'
import { formatAge } from '@/lib/format'
import { useNow } from '@/lib/now'
import { useRuns } from '@/lib/queries'
import { IssueLink } from './IssueLink'
import { Panel } from './Panel'

function runsErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    return `Runs unavailable: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Server unreachable'
}

// The tick writes "" when it could not measure the session of a run.
function standText(stand: string): string {
  return stand === '' ? 'not measured' : stand
}

// at is the tick that wrote the row: the row is at most that old.
function tickText(at: string, now: number): string {
  return `tick ${formatAge(Math.max(0, (now - Date.parse(at)) / 1000))} ago`
}

function Field({ label, children }: { label: string; children: JSX.Element | string }): JSX.Element {
  return (
    <span>
      <span className="text-muted-foreground">{label}</span> {children}
    </span>
  )
}

export function RunsList(): JSX.Element {
  const { data, error, isPending, } = useRuns()
  const deferredId = useId()
  const now = useNow()
  return (
    <Panel
      id="running"
      title="Running runs"
      icon={Play}
      tone="blue"
      error={error}
      errorText={runsErrorText}
      loading={isPending && !error}
    >
      {data && data.runs.length === 0 && <p>No running runs</p>}
      {data && data.runs.length > 0 && (
        <ul className="space-y-4">
          {data.runs.map((r) => (
            <li key={`${r.repo}#${r.issue}`} className="space-y-1">
              <p>
                <IssueLink repo={r.repo} issue={r.issue} />{' '}
                <span>{r.title}</span>
              </p>
              <p className="flex flex-wrap gap-x-4">
                <span>{r.state}</span>
                <span>{standText(r.stand)}</span>
                <span className="text-muted-foreground">{tickText(r.at, now)}</span>
              </p>
              <p className="flex flex-wrap gap-x-4">
                <Field label="command">
                  <code>{r.command}</code>
                </Field>
                <Field label="trigger">{r.trigger}</Field>
                <Field label="profile">{r.profile}</Field>
                <Field label="workspace">
                  <code>{r.workspace_id}</code>
                </Field>
              </p>
            </li>
          ))}
        </ul>
      )}
      {data && data.deferred.length > 0 && (
        <div className="space-y-2">
          <h3 id={deferredId} className="font-medium">
            Deferred starts
          </h3>
          <ul aria-labelledby={deferredId} className="space-y-4">
            {data.deferred.map((d) => (
              <li key={`${d.repo}#${d.issue}`} className="space-y-1">
                <p>
                  <IssueLink repo={d.repo} issue={d.issue} />{' '}
                  <span>{d.title}</span>
                </p>
                <p className="flex flex-wrap gap-x-4">
                  <span>{d.reason}</span>
                  <span className="text-muted-foreground">{tickText(d.at, now)}</span>
                </p>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Panel>
  )
}
