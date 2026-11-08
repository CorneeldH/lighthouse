import type { JSX } from 'react'
import { splitDeferred } from '@/lib/deferred'
import { useCleanup, useQuestions, useReturned, useRuns } from '@/lib/queries'
import { type Tone, toneClass } from './tone'

interface Tile {
  id: string
  label: string
  tone: Tone
  // undefined while loading or failed.
  count: number | undefined
}

export function Summary(): JSX.Element {
  const questions = useQuestions()
  const returned = useReturned()
  const runs = useRuns()
  const cleanup = useCleanup()
  const split = runs.data ? splitDeferred(runs.data.deferred) : undefined
  const tiles: Tile[] = [
    { id: 'questions', label: 'Questions', tone: 'purple', count: questions.data?.length },
    { id: 'returned', label: 'Returned', tone: 'orange', count: returned.data?.length },
    { id: 'running', label: 'Running', tone: 'blue', count: runs.data?.runs.filter((r) => r.state === 'open').length },
    { id: 'parked', label: 'Parked', tone: 'grey', count: runs.data?.runs.filter((r) => r.state === 'parked').length },
    { id: 'queue', label: 'Queue', tone: 'yellow', count: split?.queue.length },
    { id: 'night', label: 'Night', tone: 'navy', count: split?.night.length },
    { id: 'cleanup', label: 'To clean up', tone: 'green', count: cleanup.data?.length },
  ]
  return (
    <nav aria-label="Summary" className="grid grid-cols-4 gap-2 sm:grid-cols-7">
      {tiles.map((t) => (
        <a
          key={t.id}
          href={`#${t.id}`}
          aria-label={`${t.label}: ${t.count ?? 'unavailable'}`}
          className="flex flex-col items-center gap-1 rounded-lg border border-border bg-card px-1 py-2 text-center text-xs"
        >
          <span className={`min-w-8 rounded-md px-2 py-0.5 text-base font-semibold ${toneClass[t.tone]}`}>
            {t.count === undefined ? '–' : t.count}
          </span>
          <span>{t.label}</span>
        </a>
      ))}
    </nav>
  )
}
