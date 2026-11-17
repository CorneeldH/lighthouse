import type { JSX } from 'react'
import { IssueLink } from './IssueLink'

// RunSubject names what a run, deferral or workspace belongs to: the issue as a link, or for a
// scheduled run its schedule name, which has no issue to link to.
export function RunSubject({ repo, issue, schedule }: { repo: string; issue: number; schedule?: string }): JSX.Element {
  if (schedule) {
    return <span title="scheduled run">{`${repo} · scheduled ${schedule}`}</span>
  }
  return <IssueLink repo={repo} issue={issue} />
}
