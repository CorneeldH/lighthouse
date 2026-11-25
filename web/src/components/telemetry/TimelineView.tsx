import type { JSX } from 'react'
import type { Week } from '@/api/lighthouse'
import { forgeErrorText, type TelemetryFilter } from '@/lib/api'
import { useTelemetryTimeline } from '@/lib/queries'
import { money } from './format'

const W = 640
const H = 180
const PAD = { top: 16, right: 12, bottom: 28, left: 12 }
const PLOT_W = W - PAD.left - PAD.right
const PLOT_H = H - PAD.top - PAD.bottom

const max = (values: (number | null)[]): number => Math.max(1, ...values.map((v) => v ?? 0))
const cell = (v: number | null): string => (v === null ? 'not measured' : String(v))

// Draws only; the numbers come from the server. A null week is a gap, 0 is a zero-height bar.
export function TimelineView({ filter }: { filter: TelemetryFilter }): JSX.Element {
  const q = useTelemetryTimeline(filter)
  const weeks: Week[] = q.data?.weeks ?? []
  const markers = q.data?.markers ?? []
  const step = weeks.length > 0 ? PLOT_W / weeks.length : PLOT_W
  const barW = Math.max(2, step * 0.6)
  const xMid = (i: number): number => PAD.left + step * i + step / 2
  const costMax = max(weeks.map((w) => w.cost))
  const qMax = max(weeks.map((w) => w.questions))
  const costY = (v: number): number => (PLOT_H * v) / costMax
  const qY = (v: number): number => PAD.top + PLOT_H - (PLOT_H * v) / qMax

  // The questions line breaks at a null week.
  const segments: string[] = []
  let current: string[] = []
  const flush = (): void => {
    if (current.length > 1) segments.push(current.join(' '))
    current = []
  }
  weeks.forEach((w, i) => {
    if (w.questions === null) {
      flush()
    } else {
      current.push(`${xMid(i)},${qY(w.questions)}`)
    }
  })
  flush()

  return (
    <div className="space-y-2">
      {q.error !== null && q.error !== undefined && <p role="alert">{forgeErrorText(q.error)}</p>}
      {(q.isPending || q.isPlaceholderData) && <p>Loading…</p>}
      {q.data && weeks.length === 0 && <p>No data yet</p>}
      {q.data && weeks.length > 0 && (
        <>
          <svg
            role="img"
            aria-label="Cost per week as bars and questions per week as a line"
            viewBox={`0 0 ${W} ${H}`}
            className="w-full max-w-3xl text-foreground"
          >
            <line x1={PAD.left} x2={W - PAD.right} y1={PAD.top + PLOT_H} y2={PAD.top + PLOT_H} stroke="currentColor" strokeOpacity={0.3} />
            {weeks.map((w, i) =>
              w.cost === null ? null : (
                <rect
                  key={w.start}
                  data-week={w.start}
                  x={xMid(i) - barW / 2}
                  y={PAD.top + PLOT_H - costY(w.cost)}
                  width={barW}
                  height={costY(w.cost)}
                  className="fill-lh-navy dark:fill-sky-400"
                />
              ),
            )}
            {segments.map((points, i) => (
              <polyline key={i} points={points} fill="none" strokeWidth={2} className="stroke-amber-600 dark:stroke-amber-400" />
            ))}
            {weeks.map((w, i) =>
              w.questions === null ? null : (
                <circle key={w.start} data-questions={w.start} cx={xMid(i)} cy={qY(w.questions)} r={3} className="fill-amber-600 dark:fill-amber-400" />
              ),
            )}
            {markers.map((m, i) => {
              const idx = weeks.findIndex((w) => w.start === m.week)
              if (idx < 0) return null
              const x = xMid(idx)
              const flip = x > W * 0.7
              return (
                <g key={`${m.week}-${i}`}>
                  <line data-marker={m.kind} x1={x} x2={x} y1={PAD.top} y2={PAD.top + PLOT_H} stroke="currentColor" strokeDasharray="3 3" strokeOpacity={0.7} />
                  <text
                    x={x + (flip ? -3 : 3)}
                    y={PAD.top + 8}
                    fontSize={10}
                    textAnchor={flip ? 'end' : 'start'}
                    fill="currentColor"
                  >
                    {m.text}
                  </text>
                </g>
              )
            })}
            <text x={PAD.left} y={H - 8} fontSize={10} fill="currentColor">
              {weeks[0].start}
            </text>
            <text x={W - PAD.right} y={H - 8} fontSize={10} textAnchor="end" fill="currentColor">
              {weeks[weeks.length - 1].start}
            </text>
          </svg>
          <p className="text-xs text-muted-foreground">
            <span className="text-lh-navy dark:text-sky-400">Bars</span>: cost per week (max {Math.round(costMax * 100) / 100}).{' '}
            <span className="text-amber-600 dark:text-amber-400">Line</span>: questions per week (max {Math.round(qMax * 100) / 100}).
          </p>
          <table className="sr-only">
            <caption>Telemetry per week</caption>
            <thead>
              <tr>
                <th scope="col">Week</th>
                <th scope="col">Cost</th>
                <th scope="col">Questions</th>
                <th scope="col">Closed</th>
                <th scope="col">Recovered</th>
              </tr>
            </thead>
            <tbody>
              {weeks.map((w) => (
                <tr key={w.start}>
                  <th scope="row">{w.start}</th>
                  <td>{w.cost === null ? cell(null) : money(w.cost)}</td>
                  <td>{cell(w.questions)}</td>
                  <td>{cell(w.closed)}</td>
                  <td>{cell(w.recovered)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <table aria-label="Closed and recovered per week" className="text-sm">
            <caption className="text-left text-xs text-muted-foreground">Trajectories closed per week and how many of them needed recovery</caption>
            <thead>
              <tr className="text-left">
                <th scope="col" className="pr-4">Week</th>
                <th scope="col" className="pr-4">Closed</th>
                <th scope="col">Recovered</th>
              </tr>
            </thead>
            <tbody>
              {weeks.map((w) => (
                <tr key={w.start}>
                  <th scope="row" className="pr-4 font-normal">{w.start}</th>
                  <td className="pr-4">{cell(w.closed)}</td>
                  <td>{cell(w.recovered)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {markers.length > 0 && (
            <ul className="sr-only">
              {markers.map((m, i) => (
                <li key={`${m.week}-${i}`}>
                  {m.week}: {m.text}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  )
}
