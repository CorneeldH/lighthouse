import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ThemeToggle } from './ThemeToggle'

afterEach(() => {
  cleanup()
  localStorage.clear()
  delete document.documentElement.dataset.theme
})

describe('ThemeToggle', () => {
  it('has System pressed by default and sets no data-theme', () => {
    render(<ThemeToggle />)
    expect(screen.getByRole('group', { name: 'Theme' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'System' }).getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByRole('button', { name: 'Dark' }).getAttribute('aria-pressed')).toBe('false')
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })

  it('applies and stores a click on Dark', () => {
    render(<ThemeToggle />)
    fireEvent.click(screen.getByRole('button', { name: 'Dark' }))
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('lighthouse.theme')).toBe('dark')
    expect(screen.getByRole('button', { name: 'Dark' }).getAttribute('aria-pressed')).toBe('true')
  })

  it('shows a stored Light after a re-render', () => {
    localStorage.setItem('lighthouse.theme', 'light')
    render(<ThemeToggle />)
    expect(screen.getByRole('button', { name: 'Light' }).getAttribute('aria-pressed')).toBe('true')
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
