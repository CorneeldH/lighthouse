import { execFile, spawn, type ChildProcess } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'
import { test as base } from '@playwright/test'
import { FAKE_TOKEN, startFakeGitHub, type FakeGitHub } from './fake-github.ts'

export { expect } from '@playwright/test'

export interface StartOptions {
  // Give serve GH_TOKEN; without it the forge is not configured (503).
  token: boolean
  // The Claude usage cache: measured now, an hour ago (stale), or absent.
  cache: 'fresh' | 'old' | 'none'
  // Seed lighthouse.db with one trajectory, review and recovery before serve starts.
  telemetry?: boolean
}

export interface Lighthouse {
  // Starts lighthouse serve and returns its base URL.
  start(opts: StartOptions): Promise<string>
}

// The repository root, where `go run` finds the e2e fixture of the telemetry tab.
const REPO_ROOT = fileURLToPath(new URL('../..', import.meta.url))
const SEED_TIMEOUT_MS = 120_000

// seedTelemetry migrates db and writes the telemetry fixture into it (internal/telemetry/report/e2efixture).
async function seedTelemetry(db: string): Promise<void> {
  await promisify(execFile)('go', ['run', './internal/telemetry/report/e2efixture', db], {
    cwd: REPO_ROOT,
    // The same build as the binary of CI (CGO_ENABLED=0), so go run reuses its cache.
    env: { ...process.env, CGO_ENABLED: '0' },
    timeout: SEED_TIMEOUT_MS,
  })
}

const LISTENING = /lighthouse: listening on (\S+)\n/
const START_TIMEOUT_MS = 10_000
const STOP_TIMEOUT_MS = 15_000

// The cache form of internal/usage/state.go: UTC, whole seconds.
function utc(ms: number): string {
  return new Date(ms).toISOString().replace(/\.\d{3}Z$/, 'Z')
}

async function writeCache(xdg: string, cache: 'fresh' | 'old'): Promise<void> {
  const now = Date.now()
  const generated = cache === 'fresh' ? now : now - 3_600_000
  const dir = join(xdg, 'lighthouse')
  await mkdir(dir, { recursive: true })
  const state = {
    schema: 1,
    generated_at: utc(generated),
    // Different values per window, so a swap shows.
    session: { pct: 42, reset_at: utc(now + 2 * 3_600_000), reset_local: '' },
    week: { pct: 73, reset_at: utc(now + 3 * 86_400_000), reset_local: '' },
  }
  await writeFile(join(dir, 'usage-window.json'), JSON.stringify(state) + '\n')
}

// listenAddress resolves with the address serve prints on stderr, or rejects when serve
// exits or stays silent.
function listenAddress(proc: ChildProcess): Promise<string> {
  return new Promise((resolve, reject) => {
    let stderr = ''
    const timer = setTimeout(
      () => reject(new Error(`no start line within ${START_TIMEOUT_MS} ms; stderr: ${stderr}`)),
      START_TIMEOUT_MS,
    )
    proc.stderr?.on('data', (chunk: Buffer) => {
      stderr += chunk.toString()
      const m = LISTENING.exec(stderr)
      if (m) {
        clearTimeout(timer)
        resolve(m[1])
      }
    })
    proc.on('exit', (code, signal) => {
      clearTimeout(timer)
      reject(new Error(`serve exited early (code ${code}, signal ${signal}); stderr: ${stderr}`))
    })
    proc.on('error', (err) => {
      clearTimeout(timer)
      reject(err)
    })
  })
}

async function stop(proc: ChildProcess): Promise<void> {
  if (proc.exitCode !== null || proc.signalCode !== null) return
  const exited = new Promise<void>((resolve) => proc.once('exit', () => resolve()))
  proc.kill('SIGTERM')
  const timer = setTimeout(() => proc.kill('SIGKILL'), STOP_TIMEOUT_MS)
  await exited
  clearTimeout(timer)
}

export const test = base.extend<{ github: FakeGitHub; lighthouse: Lighthouse }>({
  // eslint-disable-next-line no-empty-pattern -- Playwright requires the destructuring form
  github: async ({}, use) => {
    const github = await startFakeGitHub()
    await use(github)
    await github.close()
  },

  lighthouse: async ({ github }, use) => {
    const bin = process.env.LIGHTHOUSE_BIN
    if (!bin) throw new Error('set LIGHTHOUSE_BIN to a lighthouse binary built with the SPA')
    const root = await mkdtemp(join(tmpdir(), 'lighthouse-e2e-'))
    const procs: ChildProcess[] = []

    await use({
      async start({ token, cache, telemetry }) {
        const home = join(root, 'home')
        const path = join(root, 'path')
        const xdg = join(root, 'xdg')
        await mkdir(home, { recursive: true })
        await mkdir(path, { recursive: true })
        await mkdir(xdg, { recursive: true })
        if (cache !== 'none') await writeCache(xdg, cache)
        const db = join(root, 'l.db')
        if (telemetry) await seedTelemetry(db)

        // Built from scratch, never from process.env: no real token, Codex or cache leaks in.
        // An empty HOME and PATH make Codex not measurable (no app-server, no rollout logs).
        const env: Record<string, string> = { HOME: home, XDG_CACHE_HOME: xdg, PATH: path }
        if (token) env.GH_TOKEN = FAKE_TOKEN

        const proc = spawn(
          bin,
          // A short overview interval: serve rebuilds from the fake forge within a test, not after 2 minutes.
          [
            'serve',
            '--addr',
            '127.0.0.1:0',
            '--db',
            db,
            '--repo',
            'test/repo',
            '--forge-url',
            github.url,
            '--overview-interval',
            '200ms',
          ],
          { env, stdio: ['ignore', 'ignore', 'pipe'] },
        )
        procs.push(proc)
        return `http://${await listenAddress(proc)}`
      },
    })

    await Promise.all(procs.map(stop))
    await rm(root, { recursive: true, force: true })
  },
})
