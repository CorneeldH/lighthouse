import type { FakeIssue } from './fake-github.ts'
import { expect, test } from './fixtures.ts'

// Real protocol headers, so the test also exercises the classification in the server.
const QUESTION_AB = '🤖 Vraag · fase plan\n\nKies A of B?'
const QUESTION_CD = '🤖 Vraag · fase plan\n\nKies C of D?'
const RETURNED = '🤖 Teruggegeven\nreden: geen worktree'

const FULL: FakeIssue[] = [
  { number: 11, title: 'First question', comments: [QUESTION_AB] },
  { number: 12, title: 'Second question', comments: [QUESTION_CD] },
  { number: 13, title: 'Handed back', comments: [RETURNED] },
]

const NOT_CONFIGURED = 'Forge not configured — set GH_TOKEN and --repo'

test('full: both lists and the usage windows', async ({ page, github, lighthouse }) => {
  github.set({ issues: FULL })
  await page.goto(await lighthouse.start({ token: true, cache: 'fresh' }))

  const questions = page.getByRole('region', { name: 'Open questions' })
  const first = questions.getByRole('listitem').filter({ hasText: '#11' })
  await expect(first.getByRole('link', { name: 'First question' })).toHaveAttribute(
    'href',
    'https://github.com/test/repo/issues/11',
  )
  await expect(first.getByText('Kies A of B?')).toBeVisible()
  await expect(first.getByRole('link', { name: 'Answer' })).toHaveAttribute(
    'href',
    'https://github.com/test/repo/issues/11#issuecomment-1101',
  )
  const second = questions.getByRole('listitem').filter({ hasText: '#12' })
  await expect(second.getByRole('link', { name: 'Second question' })).toHaveAttribute(
    'href',
    'https://github.com/test/repo/issues/12',
  )
  await expect(second.getByText('Kies C of D?')).toBeVisible()

  const returned = page.getByRole('region', { name: 'Returned' })
  const handedBack = returned.getByRole('listitem').filter({ hasText: '#13' })
  await expect(handedBack.getByRole('link', { name: 'Handed back' })).toHaveAttribute(
    'href',
    'https://github.com/test/repo/issues/13',
  )
  await expect(handedBack.getByText('geen worktree')).toBeVisible()

  const claudeSession = page.getByRole('group', { name: 'Claude session' })
  await expect(claudeSession).toContainText('42%')
  await expect(claudeSession).toContainText('resets')
  await expect(page.getByRole('group', { name: 'Claude week' })).toContainText('73%')
  const codexSession = page.getByRole('group', { name: 'Codex session' })
  await expect(codexSession).toContainText('not measurable')
  await expect(codexSession).not.toContainText(/%/)
})

test('empty: no issues and an old Claude cache', async ({ page, github, lighthouse }) => {
  github.set({ issues: [] })
  await page.goto(await lighthouse.start({ token: true, cache: 'old' }))

  await expect(page.getByRole('region', { name: 'Open questions' })).toContainText('No open questions')
  await expect(page.getByRole('region', { name: 'Returned' })).toContainText('No returned issues')
  await expect(page.getByRole('group', { name: 'Claude session' })).toContainText('stale')
})

test('502: the forge answers 500', async ({ page, github, lighthouse }) => {
  github.set({ status: 500 })
  await page.goto(await lighthouse.start({ token: true, cache: 'none' }))

  for (const name of ['Open questions', 'Returned']) {
    await expect(page.getByRole('region', { name }).getByRole('alert')).toContainText('Forge unreachable')
  }
  await expect(page.getByRole('group', { name: 'Claude session' })).toContainText('not measurable')
})

test('503: serve has no token, even when the test process has one', async ({ page, github, lighthouse }) => {
  github.set({ issues: FULL })
  const saved = { GITHUB_TOKEN: process.env.GITHUB_TOKEN, GH_TOKEN: process.env.GH_TOKEN }
  process.env.GITHUB_TOKEN = 'x'
  process.env.GH_TOKEN = 'x'
  try {
    await page.goto(await lighthouse.start({ token: false, cache: 'fresh' }))
  } finally {
    for (const [key, value] of Object.entries(saved)) {
      if (value === undefined) delete process.env[key]
      else process.env[key] = value
    }
  }

  for (const name of ['Open questions', 'Returned']) {
    await expect(page.getByRole('region', { name }).getByRole('alert')).toHaveText(NOT_CONFIGURED)
  }
})

test('refresh: a new question appears without a reload', async ({ page, github, lighthouse }) => {
  github.set({ issues: [] })
  await page.goto(await lighthouse.start({ token: true, cache: 'fresh' }))
  const questions = page.getByRole('region', { name: 'Open questions' })
  await expect(questions).toContainText('No open questions')
  // A reload or navigation would drop this property.
  await page.evaluate(() => {
    ;(globalThis as unknown as Record<string, unknown>).e2eSamePage = true
  })

  github.set({ issues: [FULL[0]] })
  await page.getByRole('button', { name: 'Refresh' }).click()

  await expect(questions.getByText('Kies A of B?')).toBeVisible()
  expect(await page.evaluate(() => (globalThis as unknown as Record<string, unknown>).e2eSamePage)).toBe(true)
})

test('old data stays when the forge fails on refresh', async ({ page, github, lighthouse }) => {
  github.set({ issues: FULL })
  await page.goto(await lighthouse.start({ token: true, cache: 'fresh' }))
  const questions = page.getByRole('region', { name: 'Open questions' })
  await expect(questions.getByText('Kies A of B?')).toBeVisible()

  github.set({ status: 500 })
  await page.getByRole('button', { name: 'Refresh' }).click()

  await expect(questions.getByRole('alert')).toContainText('Forge unreachable')
  await expect(questions.getByText('Kies A of B?')).toBeVisible()
})
