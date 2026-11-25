import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TelemetryFilter } from '@/lib/api'
import { newQueryClient } from '@/lib/queries'
import { ComponentsView } from './ComponentsView'

const clients: ReturnType<typeof newQueryClient>[] = []

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((c) => c.clear())
  vi.unstubAllGlobals()
})

function Host() {
  const [filter, setFilter] = useState<TelemetryFilter>({})
  return <ComponentsView filter={filter} onFilterChange={setFilter} />
}

describe('ComponentsView', () => {
  it('keeps the FilterBar mounted while the data of a new filter loads', async () => {
    const options = { areas: null, harnesses: null, models: null, pr_sizes: null, repos: null, routes: ['1', '2'], sizes: null }
    const body = JSON.stringify({ blocks: [], options })
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => {
      release = r
    })
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      const respond = () => new Response(body, { status: 200, headers: { 'Content-Type': 'application/json' } })
      return String(url).includes('route=2') ? gate.then(respond) : Promise.resolve(respond())
    })
    vi.stubGlobal('fetch', fetchMock)
    const client = newQueryClient()
    clients.push(client)
    render(
      <QueryClientProvider client={client}>
        <Host />
      </QueryClientProvider>,
    )
    const select = await screen.findByLabelText('Route')
    fireEvent.change(select, { target: { value: '2' } })
    await waitFor(() => expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('route=2'))).toBe(true))
    expect(screen.getByLabelText('Route')).toBe(select)
    release()
    await waitFor(() => expect(screen.queryByText('Loading…')).toBeNull())
    expect(screen.getByLabelText('Route')).toBe(select)
  })
})
