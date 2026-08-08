import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { historicalEnvelope, lineEnvelope, stationEnvelope } from '../fixtures/api'
import { HttpResponse } from '../test/http-response'
import { AlertHistoryPage } from './AlertHistoryPage'

let requests: URL[]
let historyResponse: unknown
let historyStatus: number

function LocationProbe() {
  return <output aria-label="location">{useLocation().search}</output>
}

function renderPage(entry = '/alerts') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  const result = render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[entry]}><Routes>
    <Route path="/alerts" element={<><AlertHistoryPage /><LocationProbe /></>} />
  </Routes></MemoryRouter></QueryClientProvider>)
  return { ...result, client }
}

describe('AlertHistoryPage', () => {
  beforeEach(() => {
    requests = []
    historyResponse = historicalEnvelope
    historyStatus = 200
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      requests.push(url)
      if (url.pathname.endsWith('/lines')) return Promise.resolve(new HttpResponse(lineEnvelope))
      if (url.pathname.endsWith('/stations')) return Promise.resolve(new HttpResponse(stationEnvelope))
      return Promise.resolve(new HttpResponse(historyResponse, { status: historyStatus }))
    }))
  })

  it('applies URL-backed filters, sends exact UTC request values, and preserves them across pagination', async () => {
    historyResponse = { ...historicalEnvelope, meta: { ...historicalEnvelope.meta, total: 51, total_pages: 3 } }
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('option', { name: 'Belgrave' })
    await user.selectOptions(screen.getByLabelText('Rail line'), 'route:belgrave/1')
    await user.selectOptions(screen.getByLabelText('Station'), 'station:richmond')
    await user.type(screen.getByLabelText('Cause'), 'CONSTRUCTION')
    await user.type(screen.getByLabelText('Effect'), 'NO_SERVICE')
    fireEvent.change(screen.getByLabelText('Start date'), { target: { value: '2026-07-01' } })
    fireEvent.change(screen.getByLabelText('End date'), { target: { value: '2026-08-01' } })
    await user.click(screen.getByRole('button', { name: 'Apply filters' }))

    await waitFor(() => expect(requests.some((url) => url.pathname.endsWith('/alerts') &&
      url.searchParams.get('status') === 'historical' && url.searchParams.get('line_id') === 'route:belgrave/1' &&
      url.searchParams.get('station_id') === 'station:richmond' && url.searchParams.get('cause') === 'CONSTRUCTION' &&
      url.searchParams.get('effect') === 'NO_SERVICE' && url.searchParams.get('from') === '2026-07-01T00:00:00.000Z' &&
      url.searchParams.get('to') === '2026-08-02T00:00:00.000Z' && url.searchParams.get('page') === '1' &&
      url.searchParams.get('page_size') === '25')).toBe(true))
    expect(screen.getByLabelText('location')).toHaveTextContent('line_id=route%3Abelgrave%2F1')
    expect(screen.getByLabelText('location')).toHaveTextContent('from=2026-07-01')
    expect(screen.getByLabelText('location')).toHaveTextContent('to=2026-08-01')
    expect(screen.getByText('51 total')).toBeVisible()

    historyResponse = { ...historicalEnvelope, meta: { ...historicalEnvelope.meta, total: 51, page: 2, total_pages: 3 } }
    await user.click(screen.getByRole('button', { name: 'Next page' }))
    await waitFor(() => expect(screen.getByLabelText('location')).toHaveTextContent('page=2'))
    const pageTwo = [...requests].reverse().find((url) => url.pathname.endsWith('/alerts'))
    expect(pageTwo?.searchParams.get('page')).toBe('2')
    expect(pageTwo?.searchParams.get('line_id')).toBe('route:belgrave/1')
    expect(pageTwo?.searchParams.get('from')).toBe('2026-07-01T00:00:00.000Z')
    expect(pageTwo?.searchParams.get('to')).toBe('2026-08-02T00:00:00.000Z')
  })

  it('blocks an invalid date range before querying', async () => {
    renderPage('/alerts?from=2026-08-01&to=2026-07-01')
    expect(await screen.findByRole('alert')).toHaveTextContent('Start date must be before end date')
    await waitFor(() => expect(requests.some((url) => url.pathname.endsWith('/stations'))).toBe(true))
    expect(requests.some((url) => url.pathname.endsWith('/alerts'))).toBe(false)
  })

  it('shows empty results and lifecycle guidance without inferring planning status', async () => {
    historyResponse = { data: [], meta: { count: 0, status: 'historical', total: 0, page: 1, page_size: 25, total_pages: 0 } }
    renderPage()
    expect(await screen.findByText('No past alerts found')).toBeVisible()
    expect(screen.getByText(/do not label alerts as planned or unplanned/)).toBeVisible()
    expect(screen.getByText(/uses the current line and station list/)).toBeVisible()
    expect(document.title).toBe('Alert history | Transit Observatory')
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus()
  })

  it('shows an initial collection error and retries it', async () => {
    historyStatus = 400
    const user = userEvent.setup()
    renderPage()
    const error = await screen.findByRole('alert')
    expect(error).toHaveTextContent('Alert history could not be loaded')
    historyStatus = 200
    await user.click(within(error).getByRole('button', { name: 'Try again' }))
    const heading = await screen.findByRole('heading', { name: 'Belgrave trains delayed' })
    expect(within(heading).getByRole('link')).toHaveAttribute('href', '/alerts/41')
  })

  it('retains cached historical results when a refresh fails', async () => {
    const { client } = renderPage()
    expect(await screen.findByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
    historyStatus = 400
    await client.refetchQueries({ queryKey: ['alerts'], exact: false })
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not refresh alert history')
    expect(screen.getByRole('heading', { name: 'Belgrave trains delayed' })).toBeVisible()
  })
})
