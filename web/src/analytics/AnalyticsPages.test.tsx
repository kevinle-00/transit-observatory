import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { analyticsMetricLimitations } from '../api/contracts'
import { lineAnalyticsDetailEnvelope, lineAnalyticsEnvelope } from '../fixtures/api'
import { HttpResponse } from '../test/http-response'
import { AnalyticsOverviewPage } from './AnalyticsOverviewPage'
import { LineAnalyticsPage } from './LineAnalyticsPage'

let requests: URL[]
let overviewResponse: unknown
let detailResponse: unknown
let failed: boolean

function LocationProbe() {
  return <output aria-label="current location">{useLocation().search}</output>
}

function renderPage(page: 'overview' | 'detail', initialEntry: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const Page = page === 'overview' ? AnalyticsOverviewPage : LineAnalyticsPage
  return { ...render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[initialEntry]}><Routes><Route path="*" element={<><Page /><LocationProbe /></>} /></Routes></MemoryRouter></QueryClientProvider>), client }
}

describe('analytics pages', () => {
  beforeEach(() => {
    requests = []
    overviewResponse = lineAnalyticsEnvelope
    detailResponse = lineAnalyticsDetailEnvelope
    failed = false
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      requests.push(url)
      if (failed) return Promise.resolve(new HttpResponse({ error: { code: 'bad_request', message: 'Unavailable' } }, { status: 400 }))
      const detail = !url.pathname.endsWith('/analytics/lines')
      return Promise.resolve(new HttpResponse(detail ? detailResponse : overviewResponse))
    }))
  })

  it('adds last-30-day UTC defaults to the URL and updates its interval', async () => {
    const user = userEvent.setup()
    const now = new Date()
    const expectedTo = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1))
    const expectedFrom = new Date(expectedTo.getTime() - 30 * 86_400_000)
    renderPage('overview', '/analytics')
    await waitFor(() => expect(screen.getByLabelText('current location')).toHaveTextContent(`from=${expectedFrom.toISOString().slice(0, 10)}`))
    expect(screen.getByLabelText('current location')).toHaveTextContent(`to=${expectedTo.toISOString().slice(0, 10)}`)
    await user.selectOptions(screen.getByLabelText('Group by'), 'week')
    expect(screen.getByLabelText('current location')).toHaveTextContent('interval=week')
  })

  it('sends exact UTC dates and renders totals with slash-safe line links', async () => {
    renderPage('overview', '/analytics?from=2026-07-20&to=2026-07-27&interval=day')
    expect(await screen.findByRole('heading', { name: 'Belgrave' })).toBeVisible()
    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]?.searchParams.get('from')).toBe('2026-07-20T00:00:00.000Z')
    expect(requests[0]?.searchParams.get('to')).toBe('2026-07-27T00:00:00.000Z')
    const card = screen.getByRole('heading', { name: 'Belgrave' }).closest('article')
    expect(card).not.toBeNull()
    expect(within(card as HTMLElement).getByText('5', { selector: 'dd' })).toBeVisible()
    expect(within(card as HTMLElement).getByRole('link', { name: 'View analytics' })).toHaveAttribute('href', '/analytics/line?line_id=route%3Abelgrave%2F1&from=2026-07-20&to=2026-07-27&interval=day')
    expect(within(card as HTMLElement).getByRole('link', { name: 'View line' })).toHaveAttribute('href', '/lines/detail?line_id=route%3Abelgrave%2F1')
  })

  it.each([
    ['/analytics?from=2026-02-30&to=2026-03-04&interval=day', 'Enter valid start and end dates'],
    ['/analytics?from=2026-07-27&to=2026-07-20&interval=day', 'must be before'],
    ['/analytics?from=2025-01-01&to=2026-01-03&interval=day', 'no more than 366 days'],
  ])('blocks invalid ranges before fetch', async (entry, message) => {
    renderPage('overview', entry)
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(fetch).not.toHaveBeenCalled()
  })

  it('renders an accessible chart, semantic table, breakdowns, metadata and exact limitations', async () => {
    renderPage('detail', '/analytics/line?line_id=route%3Abelgrave%2F1&from=2026-07-20&to=2026-07-27&interval=day')
    expect(await screen.findByRole('img', { name: /Belgrave alerts recorded and alerts that ended by day/ })).toBeVisible()
    const table = screen.getByRole('table', { name: 'Belgrave analytics data' })
    expect(within(table).getByRole('columnheader', { name: 'Median duration' })).toBeVisible()
    expect(within(table).getByText('Not available')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Reported causes' }).parentElement).toHaveTextContent('Technical Problem3')
    expect(screen.getByRole('heading', { name: 'Reported effects' }).parentElement).toHaveTextContent('Significant Delays4')
    for (const limitation of analyticsMetricLimitations) expect(screen.getByText(limitation)).toBeVisible()
    expect(document.title).toBe('Belgrave analytics | Transit Observatory')
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus()
    await userEvent.setup().click(screen.getByText('Technical details'))
    expect(screen.getByText('2026-07-20T00:00:00Z')).toBeVisible()
  })

  it('reports zero history and omitted medians without inventing values', async () => {
    detailResponse = { ...lineAnalyticsDetailEnvelope, data: { ...lineAnalyticsDetailEnvelope.data, series: [], causes: [], effects: [] } }
    renderPage('detail', '/analytics/line?line_id=route%3Abelgrave%2F1&from=2026-07-20&to=2026-07-27&interval=day')
    expect(await screen.findByRole('heading', { name: 'No alert history' })).toBeVisible()
    expect(screen.getByText(/No alerts were found/)).toBeVisible()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getAllByText('No data for this range.')).toHaveLength(2)
  })

  it('keeps cached analytics visible when a refresh fails', async () => {
    const { client } = renderPage('overview', '/analytics?from=2026-07-20&to=2026-07-27&interval=day')
    expect(await screen.findByRole('heading', { name: 'Belgrave' })).toBeVisible()
    failed = true
    await client.refetchQueries({ queryKey: ['line-analytics'] })
    expect(await screen.findByText('Analytics could not be refreshed')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Belgrave' })).toBeVisible()
    expect(screen.getByText('Showing the last available results.')).toBeVisible()
  })

  it('shows loading and error states and requires a line identifier', async () => {
    failed = true
    const first = renderPage('detail', '/analytics/line?line_id=route%3Abelgrave%2F1&from=2026-07-20&to=2026-07-27&interval=day')
    expect(screen.getByText('Loading line analytics')).toBeVisible()
    expect(await screen.findByText('Analytics could not be loaded')).toBeVisible()
    first.unmount()
    vi.mocked(fetch).mockClear()
    renderPage('detail', '/analytics/line?from=2026-07-20&to=2026-07-27&interval=day')
    expect(await screen.findByRole('alert')).toHaveTextContent('Choose a line from the analytics overview')
    expect(fetch).not.toHaveBeenCalled()
  })
})
