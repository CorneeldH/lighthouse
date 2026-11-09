import type { JSX, ReactNode } from 'react'
import { getGetLatestCommentUrl } from '@/api/lighthouse'
import { ExternalLink } from './ExternalLink'

// IssueLink links to the serve endpoint that redirects to the newest comment of the issue;
// children default to repo#issue.
export function IssueLink({ repo, issue, children }: { repo: string; issue: number; children?: ReactNode }): JSX.Element {
  return <ExternalLink href={getGetLatestCommentUrl({ repo, issue })}>{children ?? `${repo}#${issue}`}</ExternalLink>
}
