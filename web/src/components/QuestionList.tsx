import type { JSX } from 'react'
import { useQuestions } from '@/lib/queries'
import { IssueLink } from './IssueLink'
import { Panel } from './Panel'

export function QuestionList(): JSX.Element {
  const { data, error, isPending, isFetching, dataUpdatedAt } = useQuestions()
  return (
    <Panel title="Open questions" updatedAt={dataUpdatedAt} error={error} loading={isPending && !error}
      refreshing={isFetching && !isPending}
    >
      {data && data.length === 0 && <p>No open questions</p>}
      {data && data.length > 0 && (
        <ul className="space-y-4">
          {data.map((q) => (
            <li key={`${q.repo}#${q.number}`} className="space-y-1">
              <p>
                <IssueLink repo={q.repo} issue={q.number} />{' '}
                <IssueLink repo={q.repo} issue={q.number}>
                  {q.title}
                </IssueLink>
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
