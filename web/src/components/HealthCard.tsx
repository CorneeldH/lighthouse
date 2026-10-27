import { useEffect, useState, type JSX } from 'react'
import { getHealth } from '@/api/lighthouse'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

type Health =
  | { state: 'loading' }
  | { state: 'ok'; version: string }
  | { state: 'error'; message: string }

export function HealthCard(): JSX.Element {
  const [health, setHealth] = useState<Health>({ state: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    getHealth({ signal: controller.signal })
      .then((res) => {
        if (res.status === 200) {
          setHealth({ state: 'ok', version: res.data.version })
        } else {
          setHealth({ state: 'error', message: `API unreachable (HTTP ${res.status})` })
        }
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        const reason = err instanceof Error ? err.message : String(err)
        setHealth({ state: 'error', message: `API unreachable: ${reason}` })
      })
    return () => controller.abort()
  }, [])

  return (
    <Card>
      <CardHeader>
        <CardTitle>{health.state === 'ok' ? `lighthouse ${health.version}` : 'lighthouse'}</CardTitle>
      </CardHeader>
      <CardContent>
        {health.state === 'loading' && <p>Loading…</p>}
        {health.state === 'error' && <p role="alert">{health.message}</p>}
      </CardContent>
    </Card>
  )
}
