import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { MilestoneTitle } from './MilestoneTitle'

afterEach(cleanup)

describe('MilestoneTitle', () => {
  it('links the title to the forge in a new tab', () => {
    render(<MilestoneTitle title="M" url="https://forge/m" />)
    const link = screen.getByRole('link', { name: 'M' })
    expect(link.getAttribute('href')).toBe('https://forge/m')
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.getAttribute('rel')).toContain('noreferrer')
  })

  it('renders plain text without an anchor when the url is empty or missing', () => {
    const { rerender } = render(<MilestoneTitle title="M" url="" />)
    expect(screen.queryByRole('link', { name: 'M' })).toBeNull()
    expect(screen.getByText('M')).toBeTruthy()
    rerender(<MilestoneTitle title="M" />)
    expect(screen.queryByRole('link', { name: 'M' })).toBeNull()
    expect(screen.getByText('M')).toBeTruthy()
  })
})
