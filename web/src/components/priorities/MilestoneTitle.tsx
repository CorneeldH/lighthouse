// A milestone title: a link to the forge when the API gave a url, plain text otherwise.
// The SPA never builds a forge URL itself.
export function MilestoneTitle({ title, url }: { title: string; url?: string }) {
  if (!url) return <>{title}</>
  return (
    <a href={url} target="_blank" rel="noreferrer" className="font-semibold text-lh-link hover:underline">
      {title}
    </a>
  )
}
