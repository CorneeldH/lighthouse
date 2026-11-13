import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { newQueryClient } from '@/lib/queries'
import App from './App'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('shows the blocks in the order of the page design', () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise<Response>(() => {})))
    const client = newQueryClient()
    const { container } = render(
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>,
    )

    const ids = Array.from(container.querySelectorAll('[role="region"][id]')).map((r) => r.id)
    expect(ids).toEqual(['usage', 'questions', 'returned', 'running', 'parked', 'queue', 'night', 'cleanup'])
    client.clear()
  })
})
