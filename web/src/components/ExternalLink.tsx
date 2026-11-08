import { ExternalLink as ExternalLinkIcon } from 'lucide-react'
import type { JSX, ReactNode } from 'react'

// A link that opens in a new tab; label, when set, is the accessible name.
export function ExternalLink({ href, label, children }: { href: string; label?: string; children: ReactNode }): JSX.Element {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={label}
      className="inline-flex items-center gap-1 text-lh-link underline"
    >
      {children}
      <ExternalLinkIcon aria-hidden className="size-3.5 shrink-0" />
    </a>
  )
}
