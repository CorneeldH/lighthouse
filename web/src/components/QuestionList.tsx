import { MessageCircleQuestion } from 'lucide-react'
import type { JSX } from 'react'
import { useQuestions } from '@/lib/queries'
import { ExternalLink } from './ExternalLink'
import { IssueLink } from './IssueLink'
import { Panel } from './Panel'

export function QuestionList(): JSX.Element {
  const { data, error, isPending, } = useQuestions()
  return (
    <Panel id="questions" title="Open questions" icon={MessageCircleQuestion} tone="purple" count={data?.length}
      error={error} loading={isPending && !error}
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
              <ExternalLink href={q.answer_url}>Answer</ExternalLink>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}
