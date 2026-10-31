import { spawn, type ChildProcess } from 'node:child_process'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test as base } from '@playwright/test'
import { FAKE_TOKEN, startFakeGitHub, type FakeGitHub } from './fake-github.ts'

export { expect } from '@playwright/test'

export interface StartOptions {
  // Give serve GH_TOKEN; without it the forge is not configured (503).
  token: boolean
  // The Claude usage cache: measured now, an hour ago (stale), or absent.
  cache: 'fresh' | 'old' | 'none'
}

export interface Lighthouse {
  // Starts lighthouse serve and returns its base URL.
  start(opts: StartOptions): Promise<string>
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
      async start({ token, cache }) {
        const home = join(root, 'home')
        const path = join(root, 'path')
        const xdg = join(root, 'xdg')
        await mkdir(home, { recursive: true })
        await mkdir(path, { recursive: true })
        await mkdir(xdg, { recursive: true })
        if (cache !== 'none') await writeCache(xdg, cache)

        // Built from scratch, never from process.env: no real token, Codex or cache leaks in.
        // An empty HOME and PATH make Codex not measurable (no app-server, no rollout logs).
        const env: Record<string, string> = { HOME: home, XDG_CACHE_HOME: xdg, PATH: path }
        if (token) env.GH_TOKEN = FAKE_TOKEN

        const proc = spawn(
          bin,
          ['serve', '--addr', '127.0.0.1:0', '--db', join(root, 'l.db'), '--repo', 'test/repo', '--forge-url', github.url],
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
