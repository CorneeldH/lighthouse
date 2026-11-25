import type { JSX } from 'react'
import type { FindingRow, UsageRow } from '@/api/lighthouse'
import { ApiError, forgeErrorText } from '@/lib/api'
import { useTelemetryTrajectory } from '@/lib/queries'
import { hours, money } from './format'

// by_cause is in seconds.
const show = (v: number | null): string => (v === null ? 'not measured' : hours(v))

function Usage({ rows }: { rows: UsageRow[] | null }): JSX.Element | null {
  if (rows === null || rows.length === 0) return null
  return (
    <ul className="ml-4 list-disc">
      {rows.map((u, i) => (
        <li key={`${u.model}-${i}`}>
          {`${u.model}: ${u.tokens} tokens, cost ${u.cost === null ? 'not measured' : money(u.cost)} (${u.cost_kind})`}
        </li>
      ))}
    </ul>
  )
}

function Findings({ rows }: { rows: FindingRow[] }): JSX.Element {
  return (
    <ul className="ml-4 list-disc">
      {rows.map((f, i) => (
        <li key={`${f.key}-${i}`}>
          <span>{f.key}</span> · {f.severity} · {f.decision} · <code>{f.record_path}</code>
        </li>
      ))}
    </ul>
  )
}

export function TrajectoryDetail({ id }: { id: number }): JSX.Element {
  const q = useTelemetryTrajectory(id)
  const back = (
    <button type="button" onClick={() => (window.location.hash = '#telemetry')} className="underline">
      Back to overview
    </button>
  )
  if (q.error instanceof ApiError && q.error.status === 404) {
    return (
      <div className="space-y-2">
        {back}
        <p role="alert">Trajectory not found</p>
      </div>
    )
  }
  if (q.error !== null && q.error !== undefined) {
    return (
      <div className="space-y-2">
        {back}
        <p role="alert">{forgeErrorText(q.error)}</p>
      </div>
    )
  }
  const d = q.data
  if (!d) {
    return (
      <div className="space-y-2">
        {back}
        <p>Loading…</p>
      </div>
    )
  }
  const runs = d.runs ?? []
  const sessions = d.sessions ?? []
  const reviews = d.reviews ?? []
  const recoveries = d.recoveries ?? []
  const events = d.events ?? []
  return (
    <div className="space-y-4">
      {back}
      <h3 className="text-base font-semibold">
        {d.repo} #{d.issue}
      </h3>
      <section aria-label="Runs">
        <h4 className="font-semibold">Runs</h4>
        {runs.length === 0 ? <p>None</p> : (
          <ul className="ml-4 list-disc">
            {runs.map((r) => (
              <li key={r.id}>
                #{r.id} {r.assignment} · {r.outcome} · {r.started_at} → {r.ended_at ?? 'running'}
                <Usage rows={r.usage} />
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-label="Sessions">
        <h4 className="font-semibold">Sessions</h4>
        {sessions.length === 0 ? <p>None</p> : (
          <ul className="ml-4 list-disc">
            {sessions.map((s) => (
              <li key={s.id}>
                #{s.id} {s.harness} · {s.assignment} · {s.branch} · {s.link_basis} · {s.started_at} → {s.ended_at ?? 'running'}
                <Usage rows={s.usage} />
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-label="Reviews">
        <h4 className="font-semibold">Reviews</h4>
        {reviews.length === 0 ? <p>None</p> : (
          <ul className="space-y-2">
            {reviews.map((r) => (
              <li key={r.id}>
                {r.component} · {r.outcome} · {r.ran_at} · <code>{r.record_path}</code> · {r.finding_count} findings
                <Findings rows={r.findings ?? []} />
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-label="Recovery">
        <h4 className="font-semibold">Recovery</h4>
        {recoveries.length === 0 ? <p>None</p> : (
          <ul className="space-y-2">
            {recoveries.map((r) => (
              <li key={r.pr}>
                PR #{r.pr} · {r.status} · merged {r.merged_at}
                <ul className="ml-4 list-disc">
                  {(r.items ?? []).map((it, i) => (
                    <li key={i}>
                      {it.kind} {it.ref} · {it.at}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-label="Events">
        <h4 className="font-semibold">Events</h4>
        <p>
          Questions: dispatcher: {d.questions.dispatcher}, transcript: {d.questions.transcript}
        </p>
        {events.length > 0 && (
          <ul className="ml-4 list-disc">
            {events.map((e, i) => (
              <li key={i}>
                {e.at} · {e.kind} · {e.ref}
              </li>
            ))}
          </ul>
        )}
      </section>
      <section aria-label="Time by cause">
        <h4 className="font-semibold">Time by cause</h4>
        <ul className="ml-4 list-disc">
          {Object.entries(d.by_cause).map(([cause, v]) => (
            <li key={cause}>
              {`${cause}: ${show(v)}`}
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
