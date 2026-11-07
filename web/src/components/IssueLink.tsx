import type { JSX, ReactNode } from 'react'
import { getGetLatestCommentUrl } from '@/api/lighthouse'

// IssueLink links to the serve endpoint that redirects to the newest comment of the issue;
// children default to repo#issue.
export function IssueLink({ repo, issue, children }: { repo: string; issue: number; children?: ReactNode }): JSX.Element {
  return (
    <a href={getGetLatestCommentUrl({ repo, issue })} className="underline">
      {children ?? `${repo}#${issue}`}
    </a>
  )
}
