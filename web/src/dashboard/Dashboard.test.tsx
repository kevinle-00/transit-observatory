import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse } from '../test/http-response'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { currentAlert, currentEnvelope, degradedStatus, freshStatus, lineEnvelope, upcomingAlert, upcomingEnvelope } from '../fixtures/api'
import { formatPeriods } from '../shared/format'
import { Dashboard } from './Dashboard'

interface Responses {
  status: unknown
  lines: unknown
  current: unknown
  upcoming: unknown
}

let responses: Responses
let failures: Partial<Record<keyof Responses, number>>
let requests: URL[]
let filteredResponses: Partial<Pick<Responses, 'current' | 'upcoming'>>

function LocationProbe() {
  return <output aria-label="current location">{useLocation().search}</output>
}

function responseFor(url: URL): keyof Responses {
  if (url.pathname.endsWith('/status')) return 'status'
  if (url.pathname.endsWith('/lines')) return 'lines'
  return url.searchParams.get('status') === 'upcoming' ? 'upcoming' : 'current'
}

function renderDashboard(initialEntry = '/') {
  const client = new QueryClient({ defaultOptions: { queries: { retryDelay: 0, gcTime: Infinity } } })
  const result = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes><Route path="*" element={<><Dashboard /><LocationProbe /></>} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return { ...result, client }
}

describe('Dashboard', () => {
  beforeEach(() => {
    responses = { status: freshStatus, lines: lineEnvelope, current: currentEnvelope, upcoming: upcomingEnvelope }
    failures = {}
    requests = []
    filteredResponses = {}
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      requests.push(url)
      const key = responseFor(url)
      if ((failures[key] ?? 0) > 0) {
        failures[key] = (failures[key] ?? 0) - 1
        return Promise.resolve(new HttpResponse({ error: { code: 'source_unavailable', message: 'Source is taking a break.' } }, { status: 503 }))
      }
      const filtered = url.searchParams.has('line_id') && (key === 'current' || key === 'upcoming') ? filteredResponses[key] : undefined
      return Promise.resolve(new HttpResponse(filtered ?? responses[key]))
    }))
  })

  it('renders a fresh populated service dashboard', async () => {
    renderDashboard()
    expect(await screen.findByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Coaches replace trains on Sunday' })).toBeVisible()
    expect(screen.getByLabelText('Data freshness: fresh')).toHaveTextContent('Up to date')
    expect(screen.getByText('Up to date', { selector: '.status-pill' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Whole network' })).toBeVisible()
    expect(screen.getByText('222')).toBeVisible()
    expect(document.title).toBe('Melbourne network | Transit Observatory')
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus()
    const currentAlertRow = screen.getByRole('heading', { name: 'Belgrave trains delayed' }).closest('article')
    expect(currentAlertRow).not.toBeNull()
    await userEvent.setup().click(within(currentAlertRow as HTMLElement).getByText('More details'))
    expect(within(currentAlertRow as HTMLElement).getByText('Source description')).toBeVisible()
    expect(within(currentAlertRow as HTMLElement).getByText('Richmond')).toBeVisible()
  })

  it('presents four semantic metrics with live network values', async () => {
    renderDashboard()
    const summary = await screen.findByRole('region', { name: 'Whole network' })
    expect(within(summary).getByText('Current alerts')).toBeVisible()
    expect(within(summary).getByText('Upcoming alerts')).toBeVisible()
    expect(within(summary).getByText('Regular lines')).toBeVisible()
    expect(within(summary).getByText('Stations')).toBeVisible()
    expect(within(summary).getByText('222')).toBeVisible()
  })

  it('uses one accessible segmented control with visible counts', async () => {
    const user = userEvent.setup()
    renderDashboard()
    await screen.findByRole('heading', { name: 'Belgrave trains delayed' })
    const all = screen.getByRole('radio', { name: 'All 2' })
    const current = screen.getByRole('radio', { name: 'Current 1' })
    expect(all).toBeChecked()
    expect(current.closest('label')).toHaveTextContent('Current1')
    await user.click(current)
    expect(screen.getByLabelText('current location')).toHaveTextContent('view=current')
    expect(screen.queryByRole('heading', { name: 'Upcoming alerts' })).not.toBeInTheDocument()
    expect(current).toBeChecked()
  })

  it('updates the URL, requests and selected-line summary without double encoding', async () => {
    filteredResponses.current = { data: [], meta: { count: 0, status: 'current' } }
    filteredResponses.upcoming = {
      data: [{ ...upcomingAlert, id: 91, header: [{ text: 'Filtered Belgrave works' }] }],
      meta: { count: 1, status: 'upcoming' },
    }
    const user = userEvent.setup()
    renderDashboard()
    const select = screen.getByLabelText('Rail line')
    await screen.findByRole('option', { name: 'Belgrave' })
    await user.selectOptions(select, 'route:belgrave/1')
    await waitFor(() => expect(screen.getByLabelText('current location')).toHaveTextContent('line_id=route%3Abelgrave%2F1'))
    await waitFor(() => expect(requests.some((url) => url.pathname.endsWith('/alerts') && url.searchParams.get('line_id') === 'route:belgrave/1')).toBe(true))
    expect(screen.getByRole('heading', { name: 'Belgrave line' })).toBeVisible()
    expect(within(screen.getByRole('region', { name: 'Belgrave line' })).getByText('Network lines')).toBeVisible()
    expect(within(screen.getByRole('region', { name: 'Belgrave line' })).getByText('Network stations')).toBeVisible()
    expect(await screen.findByText('No current alerts')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Filtered Belgrave works' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Belgrave trains delayed' })).not.toBeInTheDocument()
    expect(within(screen.getByLabelText('Alert filters')).getByRole('button', { name: 'Clear line filter' })).toBeVisible()
  })

  it('persists the view filter in the URL and reduces visible sections', async () => {
    const user = userEvent.setup()
    renderDashboard('/?view=current')
    expect(await screen.findByRole('heading', { name: 'Current alerts' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Upcoming alerts' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('radio', { name: /Upcoming/ }))
    expect(await screen.findByRole('heading', { name: 'Upcoming alerts' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Current alerts' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('current location')).toHaveTextContent('view=upcoming')
  })

  it('exposes an accessible loading state while requests are pending', () => {
    vi.mocked(fetch).mockImplementation(() => new Promise(() => undefined))
    renderDashboard()
    expect(screen.getByText('Checking source freshness').closest('[role="status"]')).toBeInTheDocument()
    expect(screen.getByText('Loading current alerts')).toBeVisible()
    expect(screen.getByText('Loading upcoming alerts')).toBeVisible()
  })

  it('keeps successful sections visible when current alerts fail and retries that section', async () => {
    failures.current = 2
    const user = userEvent.setup()
    renderDashboard()
    expect(await screen.findByRole('heading', { name: 'Coaches replace trains on Sunday' })).toBeVisible()
    const error = await screen.findByRole('alert', { name: '' })
    expect(error).toHaveTextContent('Current alerts could not be loaded')
    await user.click(within(error).getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
  })

  it('offers a visible retry when the line list fails', async () => {
    failures.lines = 2
    const user = userEvent.setup()
    renderDashboard()
    const retry = await screen.findByRole('button', { name: 'Retry line list' })
    expect(screen.getByText('Line list unavailable')).toBeVisible()
    await user.click(retry)
    expect(await screen.findByRole('option', { name: 'Belgrave' })).toBeInTheDocument()
  })

  it('retains a bookmarked line filter when the line list fails and allows clearing it', async () => {
    failures.lines = 2
    const user = userEvent.setup()
    renderDashboard('/?line_id=route%3Abelgrave%2F1&view=current')
    expect(await screen.findByRole('button', { name: 'Clear line filter' })).toBeVisible()
    expect(screen.getByText('route:belgrave/1')).toBeVisible()
    await waitFor(() => expect(requests.some((url) => url.pathname.endsWith('/alerts') &&
      url.searchParams.get('line_id') === 'route:belgrave/1')).toBe(true))
    expect(screen.queryByRole('heading', { name: 'Whole network' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Clear line filter' }))
    expect(await screen.findByRole('heading', { name: 'Whole network' })).toBeVisible()
  })

  it('clears an unknown bookmarked line before requesting alerts and preserves view', async () => {
    filteredResponses.current = {
      data: [{ ...currentAlert, id: 92, header: [{ text: 'Unknown line result' }] }],
      meta: { count: 1, status: 'current' },
    }
    renderDashboard('/?line_id=missing%3Aline&view=current')
    await waitFor(() => expect(screen.getByLabelText('current location')).toHaveTextContent('?view=current'))
    expect(screen.getByLabelText('current location')).not.toHaveTextContent('line_id')
    expect(screen.getByLabelText('Rail line')).toHaveValue('')
    expect(await screen.findByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Unknown line result' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Whole network' })).toBeVisible()
    expect(requests.some((url) => url.searchParams.get('line_id') === 'missing:line')).toBe(false)
    expect(requests.some((url) => url.pathname.endsWith('/alerts') && url.searchParams.get('status') === 'current' && !url.searchParams.has('line_id'))).toBe(true)
  })

  it('turns malformed nested responses into independent section errors', async () => {
    responses.status = { ...freshStatus, data: { ...freshStatus.data, static_gtfs: { counts: null } } }
    responses.lines = { ...lineEnvelope, data: [{ id: 'broken' }] }
    responses.current = { ...currentEnvelope, data: [{ ...currentAlert, header: [{ text: 7 }] }] }
    renderDashboard()
    expect(await screen.findByRole('heading', { name: 'Coaches replace trains on Sunday' })).toBeVisible()
    expect(screen.getByText('Source status is unavailable')).toBeVisible()
    expect(screen.getByText('Line list unavailable')).toBeVisible()
    expect(screen.getByText('Current alerts could not be loaded')).toBeVisible()
  })

  it('shows cached alerts with a refresh warning after a background failure', async () => {
    const { client } = renderDashboard('/?view=current')
    expect(await screen.findByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
    failures.current = 2
    await client.refetchQueries({ queryKey: ['alerts', 'current', null], exact: true })
    const warning = await screen.findByRole('alert')
    expect(warning).toHaveTextContent('Could not refresh current alerts')
    expect(screen.getByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
  })

  it('shows independent empty states for both alert classifications', async () => {
    responses.current = { data: [], meta: { count: 0, status: 'current' } }
    responses.upcoming = { data: [], meta: { count: 0, status: 'upcoming' } }
    renderDashboard()
    expect(await screen.findByText('No current alerts')).toBeVisible()
    expect(screen.getByText('No upcoming alerts')).toBeVisible()
  })

  it('shows a clear degraded freshness warning with backend reasons', async () => {
    responses.status = degradedStatus
    renderDashboard()
    const warning = await screen.findByLabelText('Data freshness warning')
    expect(warning).toHaveTextContent('Updates delayed')
    expect(warning).toHaveTextContent('Some service information may have changed')
    expect(screen.getByText('Updates delayed', { selector: '.status-pill' })).toBeVisible()
    await userEvent.setup().click(within(warning).getByText('Source checks and reasons'))
    expect(warning).toHaveTextContent('Data Stale, Recent Failure')
  })

  it('labels unmatched source identifiers and uses safe color styling', async () => {
    renderDashboard('/?view=upcoming')
    const alert = (await screen.findByRole('heading', { name: 'Coaches replace trains on Sunday' })).closest('article')
    expect(alert).not.toBeNull()
    expect(alert).toHaveTextContent('legacy:night-busunmatched')
    expect(alert).toHaveTextContent('line legacy:night-bus, station legacy-stop-7')
    expect(within(alert as HTMLElement).getByText('legacy:night-bus').closest('span')).toHaveStyle('--badge-color: #4c5560')
  })

  it('keeps very long source content in semantic wrapping containers', async () => {
    const longId = `legacy:${'source-segment-'.repeat(20)}`
    responses.upcoming = {
      data: [{
        ...upcomingAlert,
        header: [{ text: `Works affecting ${'a-very-long-description-'.repeat(15)}` }],
        routes: [{ source_route_id: longId, is_matched: false }],
      }],
      meta: { count: 1, status: 'upcoming' },
    }
    renderDashboard('/?view=upcoming')
    const identifier = await screen.findByText(longId)
    expect(identifier).toHaveClass('line-badge')
    expect(identifier.closest('.alert-card')).not.toBeNull()
    expect(screen.getByText(/could not be matched to the current network schedule/)).toHaveClass('match-note')
  })

  it('bounds all-view previews and changes the URL to view a complete kind', async () => {
    responses.current = {
      data: Array.from({ length: 7 }, (_, index) => ({ ...currentAlert, id: 100 + index, header: [{ text: `Current alert ${String(index + 1)}` }] })),
      meta: { count: 7, status: 'current' },
    }
    responses.upcoming = {
      data: Array.from({ length: 6 }, (_, index) => ({ ...upcomingAlert, id: 200 + index, header: [{ text: `Upcoming alert ${String(index + 1)}` }] })),
      meta: { count: 6, status: 'upcoming' },
    }
    const user = userEvent.setup()
    renderDashboard()
    expect(await screen.findByRole('heading', { name: 'Current alert 5' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Current alert 6' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Upcoming alert 5' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Upcoming alert 6' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'View all 7 current alerts' }))
    expect(screen.getByLabelText('current location')).toHaveTextContent('view=current')
    expect(await screen.findByRole('heading', { name: 'Current alert 7' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Upcoming alerts' })).not.toBeInTheDocument()
  })

  it('shows the active classification period instead of the first supplied period', async () => {
    const expired = { position: 0, starts_at: '2025-01-01T00:00:00Z', ends_at: '2025-01-01T01:00:00Z' }
    const active = { position: 1, starts_at: '2026-01-01T00:00:00Z', ends_at: '2027-01-01T00:00:00Z' }
    responses.current = {
      data: [{ ...currentAlert, active_periods: [expired, active] }],
      meta: { count: 1, status: 'current' },
    }
    renderDashboard('/?view=current')
    const article = (await screen.findByRole('heading', { name: 'Belgrave trains delayed' })).closest('article')
    expect(article).not.toBeNull()
    expect(within(article as HTMLElement).getByText(formatPeriods([active])[0] ?? '')).toBeVisible()
  })

  it('progressively reveals 20 more alerts and resets when the view changes', async () => {
    responses.current = {
      data: Array.from({ length: 45 }, (_, index) => ({ ...currentAlert, id: 300 + index, header: [{ text: `Large result ${String(index + 1)}` }] })),
      meta: { count: 45, status: 'current' },
    }
    const user = userEvent.setup()
    renderDashboard('/?view=current')
    expect(await screen.findByRole('heading', { name: 'Large result 20' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Large result 21' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Show 20 more' }))
    expect(screen.getByRole('heading', { name: 'Large result 40' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Large result 21' }).closest('article')).toHaveFocus()
    expect(screen.queryByRole('heading', { name: 'Large result 41' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Show 5 more' }))
    expect(screen.getByRole('heading', { name: 'Large result 45' })).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Show 20 more' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('radio', { name: 'Upcoming 1' }))
    await user.click(screen.getByRole('radio', { name: 'Current 45' }))
    expect(await screen.findByRole('heading', { name: 'Large result 20' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Large result 21' })).not.toBeInTheDocument()
  })

  it('normalizes unsupported view values without disturbing other parameters', async () => {
    renderDashboard('/?view=historical&campaign=winter')
    await screen.findByRole('heading', { name: 'Belgrave trains delayed' })
    await waitFor(() => expect(screen.getByLabelText('current location')).toHaveTextContent('?campaign=winter'))
    expect(screen.getByLabelText('current location')).not.toHaveTextContent('view=')
    expect(screen.getByRole('radio', { name: 'All 2' })).toBeChecked()
  })

  it('retries all sources from the unavailable state without losing filters', async () => {
    failures = { status: 2, lines: 2, current: 2, upcoming: 2 }
    const user = userEvent.setup()
    renderDashboard('/?view=current')
    await user.click(await screen.findByRole('button', { name: 'Retry all sources' }))
    expect(await screen.findByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
    expect(screen.getByLabelText('current location')).toHaveTextContent('view=current')
  })
})
