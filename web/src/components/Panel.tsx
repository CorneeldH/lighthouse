import type { LucideIcon } from 'lucide-react'
import type { JSX, ReactNode } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { forgeErrorText } from '@/lib/api'

export type Tone = 'purple' | 'orange' | 'blue' | 'grey' | 'yellow' | 'navy' | 'green'

const toneClass: Record<Tone, string> = {
  purple: 'bg-lh-purple text-white',
  orange: 'bg-lh-orange text-lh-navy',
  blue: 'bg-lh-blue text-white',
  grey: 'bg-lh-grey text-white',
  yellow: 'bg-lh-yellow text-lh-navy',
  navy: 'bg-lh-navy text-white',
  green: 'bg-lh-green text-white',
}

interface PanelProps {
  // Anchor for the overview.
  id: string
  title: string
  icon: LucideIcon
  tone: Tone
  count?: number
  error: unknown
  loading: boolean
  // Formats the error; defaults to the forge wording.
  errorText?: (error: unknown) => string
  children: ReactNode
}

export function Panel({
  id,
  title,
  icon: Icon,
  tone,
  count,
  error,
  loading,
  errorText = forgeErrorText,
  children,
}: PanelProps): JSX.Element {
  const hasError = error !== null && error !== undefined
  return (
    <Card id={id} role="region" aria-label={title} className="scroll-mt-4">
      <CardHeader>
        <div className="flex items-center gap-3">
          <span className={`inline-flex size-8 items-center justify-center rounded-md ${toneClass[tone]}`}>
            <Icon aria-hidden className="size-4" />
          </span>
          <CardTitle>{title}</CardTitle>
          {count !== undefined && (
            <span className="rounded-full bg-lh-navy px-2 py-0.5 text-xs font-medium text-white" aria-label={`${count} items`}>
              {count}
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {hasError && <p role="alert">{errorText(error)}</p>}
        {loading && <p>Loading…</p>}
        {children}
      </CardContent>
    </Card>
  )
}
