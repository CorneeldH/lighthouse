import type { JSX } from 'react'
import { useQuestions } from '@/lib/queries'
import { Panel } from './Panel'

export function QuestionList(): JSX.Element {
  const { data, error, isPending, dataUpdatedAt } = useQuestions()
  return (
    <Panel title="Open questions" updatedAt={dataUpdatedAt} error={error} loading={isPending && !error}>
      {data && data.length === 0 && <p>No open questions</p>}
      {data && data.length > 0 && (
        <ul className="space-y-4">
          {data.map((q) => (
            <li key={q.number} className="space-y-1">
              <p>
                <span>#{q.number}</span>{' '}
                <a href={q.url} className="underline">
                  {q.title}
                </a>
              </p>
              <p className="whitespace-pre-wrap">{q.question}</p>
              <a href={q.answer_url} className="underline">
                Answer
              </a>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}
