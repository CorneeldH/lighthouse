import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { newQueryClient } from '@/lib/queries'
import { QuestionList } from './QuestionList'
import { ReturnedList } from './ReturnedList'

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
  { number: 7, title: 'First', url: 'https://x/7', question: 'why?', answer_url: 'https://x/7#a' },
  { number: 9, title: 'Second', url: 'https://x/9', question: 'how?', answer_url: 'https://x/9#a' },
]

describe('QuestionList', () => {
  it('shows number, title link, question text and answer link per item', async () => {
    stubFetch(() => json({ questions }))
    renderWithClient(<QuestionList />)

    expect(await screen.findByText('#7')).toBeTruthy()
    expect(within(screen.getByRole('region', { name: 'Open questions' })).getByText('updated < 1 min ago')).toBeTruthy()
    expect(screen.getByText('#9')).toBeTruthy()
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
    expect(await screen.findByText('#7')).toBeTruthy()

    res = () => json({ detail: 'bad gateway' }, 502)
    await act(async () => {
      await client?.refetchQueries()
    })

    expect(screen.getByText('#7')).toBeTruthy()
    expect((await screen.findByRole('alert')).textContent).toMatch(/^Forge unreachable: /)
    expect(within(screen.getByRole('region', { name: 'Open questions' })).getByText('updated < 1 min ago')).toBeTruthy()
  })
})

describe('ReturnedList', () => {
  it('shows number, title link and reason', async () => {
    stubFetch(() =>
      json({ returned: [{ number: 3, title: 'Back', url: 'https://x/3', reason: 'unclear AC' }] }),
    )
    renderWithClient(<ReturnedList />)

    expect(await screen.findByText('#3')).toBeTruthy()
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
