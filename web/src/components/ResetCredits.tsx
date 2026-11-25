import type { JSX } from 'react'
import type { ResetCredits as ResetCreditsType } from '@/api/lighthouse'
import { formatDate } from '@/lib/format'

interface ResetCreditsProps {
  credits: ResetCreditsType | null
  now: Date
}

// Unknown credits (null) and zero available render nothing: the window bars say enough.
// `now` is part of the contract with UsagePanel; the expiry shows a date, so it is unused.
export function ResetCredits({ credits }: ResetCreditsProps): JSX.Element | null {
  if (credits === null || credits.available <= 0) return null
  const first = credits.expires[0]
  return (
    <p className="text-sm sm:col-span-2">
      {credits.available} {credits.available === 1 ? 'reset' : 'resets'} available
      {first !== undefined && (
        <span className="text-slate-500 dark:text-slate-400"> · first expires {formatDate(first)}</span>
      )}
    </p>
  )
}
