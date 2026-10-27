import type { JSX, ReactNode } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { forgeErrorText } from '@/lib/api'
import { formatAge } from '@/lib/format'
import { useNow } from '@/lib/now'

interface PanelProps {
  title: string
  // dataUpdatedAt of the query; 0 means never.
  updatedAt: number
  error: unknown
  loading: boolean
  children: ReactNode
}

export function Panel({ title, updatedAt, error, loading, children }: PanelProps): JSX.Element {
  const now = useNow()
  const hasError = error !== null && error !== undefined
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {updatedAt > 0 && (
          <p className="text-sm text-muted-foreground">
            updated {formatAge(Math.max(0, (now - updatedAt) / 1000))} ago
          </p>
        )}
      </CardHeader>
      <CardContent className="space-y-3">
        {hasError && <p role="alert">{forgeErrorText(error)}</p>}
        {loading && <p>Loading…</p>}
        {children}
      </CardContent>
    </Card>
  )
}
