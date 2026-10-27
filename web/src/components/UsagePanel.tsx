import type { JSX } from 'react'
import { useUsage } from '@/lib/queries'
import { useNow } from '@/lib/now'
import { Panel } from './Panel'
import { UsageWindow } from './UsageWindow'

export function UsagePanel(): JSX.Element {
  const { data, error, isPending, dataUpdatedAt } = useUsage()
  const now = new Date(useNow())
  return (
    <Panel title="Usage" updatedAt={dataUpdatedAt} error={error} loading={isPending && !error}>
      {data && (
        <div className="space-y-4">
          <UsageWindow label="Claude session" window={data.claude.session} now={now} />
          <UsageWindow label="Claude week" window={data.claude.week} now={now} />
          <UsageWindow label="Codex session" window={data.codex.session} now={now} />
          <UsageWindow label="Codex week" window={data.codex.week} now={now} />
        </div>
      )}
    </Panel>
  )
}
