import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { Tabs, telemetryRoute, useHashTab } from './Tabs'

function Harness() {
  const [tab, setTab] = useHashTab()
  return <Tabs active={tab} onChange={setTab} />
}

afterEach(() => {
  cleanup()
  window.location.hash = ''
})

describe('Tabs', () => {
  it('#priorities activates that tab and a click on Controller resets the hash', () => {
    window.location.hash = '#priorities'
    render(<Harness />)
    expect(screen.getByRole('tab', { name: 'Priorities' }).getAttribute('aria-selected')).toBe('true')
    fireEvent.click(screen.getByRole('tab', { name: 'Controller' }))
    expect(screen.getByRole('tab', { name: 'Controller' }).getAttribute('aria-selected')).toBe('true')
    expect(window.location.hash).not.toBe('#priorities')
  })

  it('an unknown hash falls back to Controller', () => {
    window.location.hash = '#bogus'
    render(<Harness />)
    expect(screen.getByRole('tab', { name: 'Controller' }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByRole('tab', { name: 'Priorities' }).getAttribute('aria-selected')).toBe('false')
  })

  it('follows hashchange', () => {
    render(<Harness />)
    act(() => {
      window.location.hash = '#priorities'
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(screen.getByRole('tab', { name: 'Priorities' }).getAttribute('aria-selected')).toBe('true')
  })

  it('shows three tabs in the order Controller, Priorities, Telemetry', () => {
    render(<Harness />)
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual(['Controller', 'Priorities', 'Telemetry'])
  })

  it.each(['#telemetry', '#telemetry/trajectory/7'])('%s opens the Telemetry tab', (hash) => {
    window.location.hash = hash
    render(<Harness />)
    expect(screen.getByRole('tab', { name: 'Telemetry' }).getAttribute('aria-selected')).toBe('true')
  })

  it('a click on Telemetry sets #telemetry', () => {
    render(<Harness />)
    fireEvent.click(screen.getByRole('tab', { name: 'Telemetry' }))
    expect(window.location.hash).toBe('#telemetry')
  })

  it('telemetryRoute reads the overview and the trajectory view from the hash', () => {
    window.location.hash = '#telemetry'
    expect(telemetryRoute()).toEqual({ view: 'overview' })
    window.location.hash = '#telemetry/trajectory/7'
    expect(telemetryRoute()).toEqual({ view: 'trajectory', id: 7 })
    window.location.hash = '#telemetry/trajectory/x'
    expect(telemetryRoute()).toEqual({ view: 'overview' })
  })
})
