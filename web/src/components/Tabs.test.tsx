import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { Tabs, useHashTab } from './Tabs'

function Harness() {
  const [tab, setTab] = useHashTab()
  return <Tabs active={tab} onChange={setTab} />
}

afterEach(() => {
  cleanup()
  window.location.hash = ''
})

describe('Tabs', () => {
  it('#prioritering activates that tab and a click on Controller resets the hash', () => {
    window.location.hash = '#prioritering'
    render(<Harness />)
    expect(screen.getByRole('tab', { name: 'Prioritering' }).getAttribute('aria-selected')).toBe('true')
    fireEvent.click(screen.getByRole('tab', { name: 'Controller' }))
    expect(screen.getByRole('tab', { name: 'Controller' }).getAttribute('aria-selected')).toBe('true')
    expect(window.location.hash).not.toBe('#prioritering')
  })

  it('an unknown hash falls back to Controller', () => {
    window.location.hash = '#bogus'
    render(<Harness />)
    expect(screen.getByRole('tab', { name: 'Controller' }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByRole('tab', { name: 'Prioritering' }).getAttribute('aria-selected')).toBe('false')
  })

  it('follows hashchange', () => {
    render(<Harness />)
    act(() => {
      window.location.hash = '#prioritering'
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(screen.getByRole('tab', { name: 'Prioritering' }).getAttribute('aria-selected')).toBe('true')
  })
})
