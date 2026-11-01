import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { newQueryClient } from '@/lib/queries'
import { CleanupList } from './CleanupList'
import { QuestionList } from './QuestionList'
import { ReturnedList } from './ReturnedList'
import { RunsList } from './RunsList'

let client: QueryClient | undefined

afterEach(() => {
  cleanup()
  client?.clear()
  client = undefined
  vi.unstubAllGlobals()
})

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': status === 200 ? 'application/json' : 'application/problem+json' },
  })
}

function stubFetch(make: () => Response) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(make())))
}

function renderWithClient(ui: React.ReactElement) {
  client = newQueryClient()
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

const questions = [
  { repo: 'o/a', number: 7, title: 'First', url: 'https://x/7', question: 'why?', answer_url: 'https://x/7#a' },
  { repo: 'o/b', number: 9, title: 'Second', url: 'https://x/9', question: 'how?', answer_url: 'https://x/9#a' },
]

describe('QuestionList', () => {
  it('shows repo and number, title link, question text and answer link per item', async () => {
    stubFetch(() => json({ questions }))
    renderWithClient(<QuestionList />)

    expect(await screen.findByText('o/a#7')).toBeTruthy()
    expect(within(screen.getByRole('region', { name: 'Open questions' })).getByText('updated < 1 min ago')).toBeTruthy()
    expect(screen.getByText('o/b#9')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'First' }).getAttribute('href')).toBe('https://x/7')
    expect(screen.getByRole('link', { name: 'Second' }).getAttribute('href')).toBe('https://x/9')
    expect(screen.getByText('why?')).toBeTruthy()
    expect(screen.getByText('how?')).toBeTruthy()
    const answers = screen.getAllByRole('link', { name: 'Answer' })
    expect(answers.map((a) => a.getAttribute('href'))).toEqual(['https://x/7#a', 'https://x/9#a'])
  })

  it('renders question text as text, never as markup', async () => {
    const text = 'regel 1\nregel 2 <script>x</script>'
    stubFetch(() => json({ questions: [{ ...questions[0], question: text }] }))
    const { container } = renderWithClient(<QuestionList />)

    const el = await screen.findByText((_, node) => node?.textContent === text && node.children.length === 0)
    expect(el.className).toContain('whitespace-pre-wrap')
    expect(container.querySelector('script')).toBeNull()
  })

  it('shows the empty text', async () => {
    stubFetch(() => json({ questions: [] }))
    renderWithClient(<QuestionList />)
    expect(await screen.findByText('No open questions')).toBeTruthy()
  })

  it('shows the not-configured alert on 503', async () => {
    stubFetch(() => json({ detail: 'forge not configured' }, 503))
    renderWithClient(<QuestionList />)
    expect((await screen.findByRole('alert')).textContent).toBe(
      'Forge not configured — set GH_TOKEN and --repo',
    )
    expect(screen.queryByText('No open questions')).toBeNull()
  })

  it('keeps old items and shows an alert when a refetch fails with 502', async () => {
    let res = () => json({ questions })
    stubFetch(() => res())
    renderWithClient(<QuestionList />)
    expect(await screen.findByText('o/a#7')).toBeTruthy()

    res = () => json({ detail: 'bad gateway' }, 502)
    await act(async () => {
      await client?.refetchQueries()
    })

    expect(screen.getByText('o/a#7')).toBeTruthy()
    expect((await screen.findByRole('alert')).textContent).toMatch(/^Forge unreachable: /)
    expect(within(screen.getByRole('region', { name: 'Open questions' })).getByText('updated < 1 min ago')).toBeTruthy()
  })
})

describe('ReturnedList', () => {
  it('shows repo and number, title link and reason', async () => {
    stubFetch(() =>
      json({ returned: [{ repo: 'o/a', number: 3, title: 'Back', url: 'https://x/3', reason: 'unclear AC' }] }),
    )
    renderWithClient(<ReturnedList />)

    expect(await screen.findByText('o/a#3')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back' }).getAttribute('href')).toBe('https://x/3')
    expect(screen.getByText('unclear AC')).toBeTruthy()
  })

  it('shows the empty text', async () => {
    stubFetch(() => json({ returned: [] }))
    renderWithClient(<ReturnedList />)
    expect(await screen.findByText('No returned issues')).toBeTruthy()
  })

  it('shows the not-configured alert on 503', async () => {
    stubFetch(() => json({ detail: 'x' }, 503))
    renderWithClient(<ReturnedList />)
    expect((await screen.findByRole('alert')).textContent).toBe(
      'Forge not configured — set GH_TOKEN and --repo',
    )
  })
})

describe('CleanupList', () => {
  it('shows repo, issue, workspace id, path and reason', async () => {
    stubFetch(() =>
      json({
        cleanup: [
          { repo: 'o/r', issue: 7, workspace_id: 'ws-1', path: '/w/ws-1', reason: 'dirty worktree' },
        ],
      }),
    )
    renderWithClient(<CleanupList />)

    expect(await screen.findByText('o/r#7')).toBeTruthy()
    expect(screen.getByText('ws-1')).toBeTruthy()
    const path = screen.getByText('/w/ws-1')
    expect(path.tagName).toBe('CODE')
    expect(screen.getByText('dirty worktree')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Workspaces to clean up' })).toBeTruthy()
  })

  it('shows the empty text', async () => {
    stubFetch(() => json({ cleanup: [] }))
    renderWithClient(<CleanupList />)
    expect(await screen.findByText('No workspaces to clean up')).toBeTruthy()
  })

  it('shows a neutral alert on a 500, not the forge text', async () => {
    stubFetch(() => json({ detail: 'db closed' }, 500))
    renderWithClient(<CleanupList />)
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe('Cleanup list unavailable: db closed')
    expect(alert.textContent).not.toContain('Forge unreachable')
  })
})

const run = {
  repo: 'o/a',
  issue: 3,
  title: 'Add the runs card',
  state: 'open',
  command: '/lighthouse-start-issue',
  trigger: 'board',
  profile: 'software',
  workspace_id: 'ws-3',
  started_at: '2026-11-01T09:00:00Z',
  stand: 'waiting',
  at: '2026-11-01T09:05:00Z',
}

describe('RunsList', () => {
  it('shows repo, issue, title, state, stand, command, trigger, profile and workspace', async () => {
    stubFetch(() => json({ runs: [run], deferred: [] }))
    renderWithClient(<RunsList />)

    const region = screen.getByRole('region', { name: 'Running runs' })
    await within(region).findByText('o/a#3')
    const [item] = within(region).getAllByRole('listitem')
    const texts = ['o/a#3', 'Add the runs card', 'open', 'waiting', '/lighthouse-start-issue', 'board', 'software', 'ws-3']
    for (const text of texts) {
      expect(within(item).getByText(text)).toBeTruthy()
    }
    expect(screen.queryByText('No running runs')).toBeNull()
  })

  it('shows a parked run without a stand as not measured', async () => {
    stubFetch(() => json({ runs: [{ ...run, issue: 5, state: 'parked', stand: '' }], deferred: [] }))
    renderWithClient(<RunsList />)

    expect(await screen.findByText('o/a#5')).toBeTruthy()
    expect(screen.getByText('parked')).toBeTruthy()
    expect(screen.getByText('not measured')).toBeTruthy()
  })

  it('shows a deferred start with its title and reason under Deferred starts', async () => {
    stubFetch(() =>
      json({
        runs: [run],
        deferred: [{ repo: 'o/a', issue: 4, title: 'Wait for night', reason: 'slots full', at: '2026-11-01T09:05:00Z' }],
      }),
    )
    renderWithClient(<RunsList />)

    const deferred = await screen.findByRole('list', { name: 'Deferred starts' })
    expect(within(deferred).getByText('o/a#4')).toBeTruthy()
    expect(within(deferred).getByText('Wait for night')).toBeTruthy()
    expect(within(deferred).getByText('slots full')).toBeTruthy()
    expect(within(deferred).queryByText('o/a#3')).toBeNull()
  })

  it('shows the empty text when there are no runs and no deferred starts', async () => {
    stubFetch(() => json({ runs: [], deferred: [] }))
    renderWithClient(<RunsList />)
    expect(await screen.findByText('No running runs')).toBeTruthy()
    expect(screen.queryByRole('list', { name: 'Deferred starts' })).toBeNull()
  })

  it('shows a neutral alert on a 500, not the forge text', async () => {
    stubFetch(() => json({ detail: 'db closed' }, 500))
    renderWithClient(<RunsList />)
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe('Runs unavailable: db closed')
  })
})
