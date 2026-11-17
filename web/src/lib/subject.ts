// subjectKey is a React key for a row of repo: the issue, or the schedule of a scheduled run.
export function subjectKey(repo: string, issue: number, schedule?: string): string {
  return schedule ? `${repo}:${schedule}` : `${repo}#${issue}`
}
