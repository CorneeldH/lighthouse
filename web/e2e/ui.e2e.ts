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

const NOT_CONFIGURED = 'Forge not configured — set GH_TOKEN, and --repo or [dispatch] repos in host.toml'

test('full: both lists and the usage windows', async ({ page, github, lighthouse }) => {
  github.set({ issues: FULL })
  await page.goto(await lighthouse.start({ token: true, cache: 'fresh' }))
  await expect(page).toHaveTitle('lighthouse · Controller')
  await expect(page.getByRole('region', { name: 'Controller' })).toBeVisible()

  const questions = page.getByRole('region', { name: 'Open questions' })
  const first = questions.getByRole('listitem').filter({ hasText: 'test/repo#11' })
  await expect(first.getByRole('link', { name: 'First question' })).toHaveAttribute(
    'href',
    '/api/latest-comment?repo=test%2Frepo&issue=11',
  )
  await expect(first.getByText('Kies A of B?')).toBeVisible()
  await expect(first.getByRole('link', { name: 'Answer' })).toHaveAttribute(
    'href',
    'https://github.com/test/repo/issues/11#issuecomment-1101',
  )
  const second = questions.getByRole('listitem').filter({ hasText: 'test/repo#12' })
  await expect(second.getByRole('link', { name: 'Second question' })).toHaveAttribute(
    'href',
    '/api/latest-comment?repo=test%2Frepo&issue=12',
  )
  await expect(second.getByText('Kies C of D?')).toBeVisible()

  const returned = page.getByRole('region', { name: 'Returned' })
  const handedBack = returned.getByRole('listitem').filter({ hasText: 'test/repo#13' })
  await expect(handedBack.getByRole('link', { name: 'Handed back' })).toHaveAttribute(
    'href',
    '/api/latest-comment?repo=test%2Frepo&issue=13',
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

test('an issue link redirects to the newest comment of the issue', async ({ page, github, lighthouse }) => {
  github.set({ issues: [{ number: 11, title: 'First question', comments: ['eerste', QUESTION_AB] }] })
  await page.goto(await lighthouse.start({ token: true, cache: 'fresh' }))
  const link = page.getByRole('region', { name: 'Open questions' }).getByRole('link', { name: 'test/repo#11' })
  const href = await link.getAttribute('href')
  expect(href).toBe('/api/latest-comment?repo=test%2Frepo&issue=11')

  const res = await page.request.get(new URL(href!, page.url()).href, { maxRedirects: 0 })
  expect(res.status()).toBe(302)
  expect(res.headers()['location']).toBe('https://github.com/test/repo/issues/11#issuecomment-1102')
})

test('empty: no issues and an old Claude cache', async ({ page, github, lighthouse }) => {
  github.set({ issues: [] })
  await page.goto(await lighthouse.start({ token: true, cache: 'old' }))

  await expect(page.getByRole('region', { name: 'Open questions' })).toContainText('No open questions')
  await expect(page.getByRole('region', { name: 'Returned' })).toContainText('No returned issues')
  // A fresh database has no tick state: the card reads it without the forge.
  await expect(page.getByRole('region', { name: 'Running runs' })).toContainText('No running runs')
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
  // Refresh reads the overview serve rebuilds in the background: click until that rebuild is in.
  await expect(async () => {
    await page.getByRole('button', { name: 'Refresh' }).click()
    await expect(questions.getByText('Kies A of B?')).toBeVisible({ timeout: 500 })
  }).toPass()
  expect(await page.evaluate(() => (globalThis as unknown as Record<string, unknown>).e2eSamePage)).toBe(true)
})

test('old data stays when the forge fails on refresh', async ({ page, github, lighthouse }) => {
  github.set({ issues: FULL })
  await page.goto(await lighthouse.start({ token: true, cache: 'fresh' }))
  const questions = page.getByRole('region', { name: 'Open questions' })
  await expect(questions.getByText('Kies A of B?')).toBeVisible()

  github.set({ status: 500 })
  // As above: click until the failed background rebuild reaches the page.
  await expect(async () => {
    await page.getByRole('button', { name: 'Refresh' }).click()
    await expect(questions.getByRole('alert')).toContainText('Forge unreachable', { timeout: 500 })
  }).toPass()
  await expect(questions.getByText('Kies A of B?')).toBeVisible()
})

test('the colour scheme of the system picks light or dark', async ({ page, github, lighthouse }) => {
  github.set({ issues: [] })
  const url = await lighthouse.start({ token: true, cache: 'fresh' })
  const body = page.locator('body')
  const heading = page.getByRole('heading', { level: 2, name: 'Controller' })

  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto(url)
  await expect(body).toHaveCSS('background-color', 'oklch(1 0 0)')
  await expect(heading).toHaveCSS('color', 'rgb(20, 33, 61)')

  await page.emulateMedia({ colorScheme: 'dark' })
  await expect(body).toHaveCSS('background-color', 'rgb(15, 20, 32)')
  // The dark navy (#26335a) on the dark page is unreadable; the heading takes the foreground.
  await expect(heading).toHaveCSS('color', 'rgb(232, 234, 240)')
})

test('the theme toggle forces dark or light and keeps the choice', async ({ page, github, lighthouse }) => {
  github.set({ issues: [] })
  const url = await lighthouse.start({ token: true, cache: 'fresh' })
  const body = page.locator('body')
  const heading = page.getByRole('heading', { level: 2, name: 'Controller' })
  const theme = page.getByRole('group', { name: 'Theme' })

  // Dark forced on a light system: tokens and the dark: variant both follow.
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto(url)
  await theme.getByRole('button', { name: 'Dark' }).click()
  await expect(body).toHaveCSS('background-color', 'rgb(15, 20, 32)')
  await expect(heading).toHaveCSS('color', 'rgb(232, 234, 240)')
  await page.reload()
  await expect(theme.getByRole('button', { name: 'Dark' })).toHaveAttribute('aria-pressed', 'true')
  await expect(body).toHaveCSS('background-color', 'rgb(15, 20, 32)')

  // Light forced on a dark system.
  await page.emulateMedia({ colorScheme: 'dark' })
  await theme.getByRole('button', { name: 'Light' }).click()
  await expect(body).toHaveCSS('background-color', 'oklch(1 0 0)')
  await expect(heading).toHaveCSS('color', 'rgb(20, 33, 61)')
  await page.reload()
  await expect(theme.getByRole('button', { name: 'Light' })).toHaveAttribute('aria-pressed', 'true')
  await expect(body).toHaveCSS('background-color', 'oklch(1 0 0)')

  // Back to System: the dark system wins again.
  await theme.getByRole('button', { name: 'System' }).click()
  await expect(body).toHaveCSS('background-color', 'rgb(15, 20, 32)')
})

// The trajectory internal/telemetry/report/e2efixture writes: one review, one finding, one recovery.
const FIXTURE_REPO = 'test/telemetry'
const CORRELATION = 'Observed, not caused: reviews run more often on hard issues.'

test('telemetry: tab shows components, timeline, trajectories and coverage', async ({ page, github, lighthouse }) => {
  github.set({ issues: [] })
  await page.goto(`${await lighthouse.start({ token: true, cache: 'fresh', telemetry: true })}/#telemetry`)
  await expect(page.getByRole('tab', { name: 'Telemetry' })).toHaveAttribute('aria-selected', 'true')
  for (const name of ['Components', 'Timeline', 'Trajectories', 'Coverage']) {
    await expect(page.getByRole('region', { name, exact: true })).toBeVisible()
  }

  const planReview = page.getByRole('region', { name: 'plan_review', exact: true })
  await expect(planReview).toContainText(CORRELATION)
  // The one trajectory has a plan review: it is in the "with" group with one review that found something.
  await expect(planReview.getByRole('row').filter({ has: page.getByRole('rowheader', { name: 'with', exact: true }) })).toContainText('100%')
  // Review costs are not measured in the block itself, not only in Coverage.
  await expect(planReview).toContainText('not measured — review costs are not recorded')
  await expect(planReview.getByTitle('Comparison label')).toHaveText('correlation')
  // A block without a comparison has no label badge, only its reason.
  const taskReview = page.getByRole('region', { name: 'task_review', exact: true })
  await expect(taskReview).toContainText('not measured')
  await expect(taskReview.getByTitle('Comparison label')).toHaveCount(0)

  const coverage = page.getByRole('region', { name: 'Coverage', exact: true })
  const reviewCosts = coverage.getByRole('listitem').filter({ hasText: 'review costs: not recorded' })
  await expect(reviewCosts).toContainText('not measured')

  const trajectories = page.getByRole('region', { name: 'Trajectories', exact: true })
  await expect(trajectories.getByRole('link', { name: FIXTURE_REPO })).toBeVisible()
})

test('telemetry: a trajectory row opens its detail and back', async ({ page, github, lighthouse }) => {
  github.set({ issues: [] })
  await page.goto(`${await lighthouse.start({ token: true, cache: 'fresh', telemetry: true })}/#telemetry`)
  await expect(page.getByRole('region', { name: 'plan_review', exact: true })).toContainText(CORRELATION)
  const coverage = page.getByRole('region', { name: 'Coverage', exact: true })
  await expect(coverage.getByRole('listitem').filter({ hasText: 'review costs: not recorded' })).toContainText(
    'not measured',
  )

  await page.getByRole('region', { name: 'Trajectories', exact: true }).getByRole('link', { name: FIXTURE_REPO }).click()
  await expect(page).toHaveURL(/#telemetry\/trajectory\/\d+$/)
  await expect(page.getByRole('heading', { name: `${FIXTURE_REPO} #7` })).toBeVisible()
  const reviews = page.getByRole('region', { name: 'Reviews', exact: true })
  await expect(reviews).toContainText('plan_review')
  await expect(reviews).toContainText('1 findings')
  await expect(page.getByRole('region', { name: 'Recovery', exact: true })).toContainText('PR #70 · recovered')

  await page.getByRole('button', { name: 'Back to overview' }).click()
  await expect(page).toHaveURL(/#telemetry$/)
  await expect(page.getByRole('region', { name: 'plan_review', exact: true })).toContainText(CORRELATION)
})
