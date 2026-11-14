import type { JSX } from 'react'
import { ExternalLink } from './ExternalLink'

// The workspace id, linked to its kenn-forge page when the forge url is known; nothing without an id.
export function WorkspaceLink({ id, forgeUrl }: { id: string; forgeUrl: string }): JSX.Element | null {
  if (id === '') return null
  if (forgeUrl === '') return <code>{id}</code>
  return (
    <ExternalLink href={`${forgeUrl}/workspaces/${encodeURIComponent(id)}`} label={`${id}, opens kenn-forge in a new tab`}>
      {id}
    </ExternalLink>
  )
}
