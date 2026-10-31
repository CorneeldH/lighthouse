import { createServer, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'

// A fake GitHub REST API: just the two endpoints internal/forge/github.go reads, for repo test/repo.

export interface FakeIssue {
  number: number
  title: string
  comments: string[] // comment bodies, oldest first
}

// Either the open issues, or a status every request gets.
export type Scenario = { issues: FakeIssue[] } | { status: number }

export interface FakeGitHub {
  url: string
  set(s: Scenario): void
  close(): Promise<void>
}

// The token the fixture gives serve; any other answers 401, as GitHub does.
export const FAKE_TOKEN = 'test'

const COMMENTS_PATH = /^\/repos\/test\/repo\/issues\/(\d+)\/comments$/

function issueURL(n: number): string {
  return `https://github.com/test/repo/issues/${n}`
}

// GitHub's timestamp form, without milliseconds; later comments and higher numbers are later.
function createdAt(n: number, i: number): string {
  return new Date(Date.UTC(2026, 9, 27, 10) + n * 60_000 + i * 1_000).toISOString().replace(/\.\d{3}Z$/, 'Z')
}

function send(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(body))
}

export async function startFakeGitHub(): Promise<FakeGitHub> {
  let scenario: Scenario = { issues: [] }

  const server = createServer((req, res) => {
    if ('status' in scenario) {
      send(res, scenario.status, { message: `fake status ${scenario.status}` })
      return
    }
    if (req.method !== 'GET') {
      send(res, 405, { message: 'only GET' })
      return
    }
    if (req.headers.authorization !== `Bearer ${FAKE_TOKEN}`) {
      send(res, 401, { message: 'Bad credentials' })
      return
    }
    const path = new URL(req.url ?? '/', 'http://fake').pathname
    const { issues } = scenario
    if (path === '/repos/test/repo/issues') {
      send(
        res,
        200,
        issues.map((issue) => ({
          number: issue.number,
          title: issue.title,
          html_url: issueURL(issue.number),
          comments: issue.comments.length,
        })),
      )
      return
    }
    const match = COMMENTS_PATH.exec(path)
    const issue = match && issues.find((it) => it.number === Number(match[1]))
    if (issue) {
      send(
        res,
        200,
        issue.comments.map((body, i) => ({
          body,
          created_at: createdAt(issue.number, i),
          html_url: `${issueURL(issue.number)}#issuecomment-${issue.number}0${i + 1}`,
          reactions: { rocket: 0 },
        })),
      )
      return
    }
    send(res, 404, { message: 'Not Found' })
  })

  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address() as AddressInfo
  return {
    url: `http://127.0.0.1:${port}`,
    set(s) {
      scenario = s
    },
    close() {
      server.closeAllConnections()
      return new Promise((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())))
    },
  }
}
