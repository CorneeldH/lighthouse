import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Block, Group, Measure } from '@/api/lighthouse'
import { ComparisonBlock } from './ComparisonBlock'
import { FilterBar } from './FilterBar'
import { MeasureCell } from './MeasureCell'

afterEach(cleanup)

const m = (value: number | null, n: number, reason?: string): Measure => ({ value, n, reason })

function group(name: string): Group {
  return {
    name,
    active: m(3, 12),
    adopted: m(0.5, 12),
    by_severity: {},
    cost: m(1.5, 12),
    cost_kind: 'estimated',
    findings: 4,
    found: m(2, 12),
    human_wait: m(null, 0, 'no wait recorded'),
    left_open: 0,
    questions: m(1, 12),
    recovered: m(1, 12),
    recovery_items: m(1, 12),
    reviews: 1,
    trajectories: 12,
    undecided: 0,
  }
}

function block(over: Partial<Block>): Block {
  return {
    name: 'route',
    kind: 'correlation',
    label: 'Correlation',
    label_note: 'Observed, not caused: groups differ in more than the route.',
    groups: [group('route 1')],
    outside: { before_cohort: 1, mixed_lane: 2, no_lane: 0, no_recovery: 0, running: 3 },
    ...over,
  }
}

describe('MeasureCell', () => {
  it('shows not measured with the reason as title, never 0', () => {
    render(<MeasureCell measure={m(null, 0, 'no data')} />)
    const el = screen.getByText('not measured')
    expect(el.getAttribute('title')).toBe('no data')
    expect(screen.queryByText('0')).toBeNull()
  })

  it('marks n=3 as too few trajectories and grey', () => {
    render(<MeasureCell measure={m(2, 3)} />)
    expect(screen.getByText('too few trajectories')).toBeTruthy()
    expect(screen.getByText('n=3')).toBeTruthy()
    expect(screen.getByText('2').className).toContain('text-muted-foreground')
  })

  it('does not mark n=5', () => {
    render(<MeasureCell measure={m(2, 5)} />)
    expect(screen.queryByText('too few trajectories')).toBeNull()
    expect(screen.getByText('2').className).not.toContain('text-muted-foreground')
    expect(screen.getByText('n=5')).toBeTruthy()
  })

  it('applies the format', () => {
    render(<MeasureCell measure={m(0.5, 12)} format={(v) => `${v * 100}%`} />)
    expect(screen.getByText('50%')).toBeTruthy()
  })
})

describe('ComparisonBlock', () => {
  it('shows the label and its note inside a correlation block', () => {
    render(<ComparisonBlock block={block({})} />)
    const el = screen.getByRole('region', { name: 'route' })
    expect(within(el).getByText('Correlation')).toBeTruthy()
    expect(within(el).getByText(/Observed, not caused:/)).toBeTruthy()
    expect(within(el).getByText('Outside the comparison: 1 before cohort, 2 mixed lane, 3 running')).toBeTruthy()
    expect(within(el).getByRole('table')).toBeTruthy()
  })

  it('shows the assigned note', () => {
    render(<ComparisonBlock block={block({ kind: 'assigned', label: 'Assigned', label_note: 'Assigned by lighthouse: it chose the route.' })} />)
    expect(within(screen.getByRole('region', { name: 'route' })).getByText(/Assigned by lighthouse/)).toBeTruthy()
  })

  it('shows a mixed cost kind as a warning', () => {
    const g = group('route 1')
    g.cost_kind = 'mixed'
    render(<ComparisonBlock block={block({ groups: [g] })} />)
    expect(screen.getByText('mixed', { exact: false, selector: 'span.block' })).toBeTruthy()
    expect(screen.getByRole('img', { name: 'warning' })).toBeTruthy()
  })

  it('shows no label badge on an unmeasured block without a label', () => {
    render(<ComparisonBlock block={block({ kind: 'unmeasured', label: '', label_note: '', groups: null, reason: 'no source' })} />)
    const el = screen.getByRole('region', { name: 'route' })
    expect(within(el).queryByTitle('Comparison label')).toBeNull()
    expect(within(el).getByText('no source')).toBeTruthy()
  })

  it('shows the label badge on a comparison block', () => {
    render(<ComparisonBlock block={block({})} />)
    expect(within(screen.getByRole('region', { name: 'route' })).getByTitle('Comparison label').textContent).toBe('Correlation')
  })

  it('shows the reason of a comparison block next to its table', () => {
    render(<ComparisonBlock block={block({ reason: 'not measured — review costs are not recorded' })} />)
    const el = screen.getByRole('region', { name: 'route' })
    expect(within(el).getByText('not measured — review costs are not recorded')).toBeTruthy()
    expect(within(el).getByRole('table')).toBeTruthy()
  })

  it('formats seconds as hours, ratios as percentages and cost with two decimals', () => {
    const g = group('sdd')
    g.active = m(8876, 14)
    g.human_wait = m(1800, 14)
    g.recovered = m(0.9230769230769231, 14)
    g.found = m(1, 14)
    g.cost = m(21.402165600000007, 14)
    render(<ComparisonBlock block={block({ groups: [g] })} />)
    const el = screen.getByRole('region', { name: 'route' })
    expect(within(el).getByText('2.5 h')).toBeTruthy()
    expect(within(el).getByText('0.5 h')).toBeTruthy()
    expect(within(el).getByText('92%')).toBeTruthy()
    expect(within(el).getByText('100%')).toBeTruthy()
    expect(within(el).getByText('21.40')).toBeTruthy()
  })

  it('shows only the reason for an unmeasured block', () => {
    render(<ComparisonBlock block={block({ kind: 'unmeasured', groups: null, reason: 'no data yet' })} />)
    const el = screen.getByRole('region', { name: 'route' })
    expect(within(el).getByText('no data yet')).toBeTruthy()
    expect(within(el).queryByRole('table')).toBeNull()
  })
})

describe('FilterBar', () => {
  it('calls onChange with the chosen route', () => {
    const onChange = vi.fn()
    const options = { areas: null, harnesses: null, models: null, pr_sizes: null, repos: null, routes: ['1', '2'], sizes: null }
    render(<FilterBar options={options} value={{}} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Route'), { target: { value: '2' } })
    expect(onChange).toHaveBeenCalledWith({ route: '2' })
  })
})

describe('MeasureCell boundaries', () => {
  it('shows a real 0 as 0, not as not measured', () => {
    render(<MeasureCell measure={m(0, 12)} />)
    expect(screen.getByText('0')).toBeTruthy()
    expect(screen.queryByText('not measured')).toBeNull()
  })

  it('treats n=4 as too few and n=5 as enough', () => {
    render(<MeasureCell measure={m(7, 4)} />)
    expect(screen.getByText('too few trajectories')).toBeTruthy()
    expect(screen.getByText('7').className).toContain('text-muted-foreground')
  })
})
