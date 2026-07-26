import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { alertDetailEnvelope, alertRevisionsEnvelope } from '../fixtures/api'
import { HttpResponse } from '../test/http-response'
import { AlertDetailPage } from './AlertDetailPage'

let requests: URL[]
let detailResponse: unknown
let revisionsResponse: unknown
let revisionsStatus: number
let detailStatus: number

function renderPage(id: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/alerts/${id}`]}><Routes>
    <Route path="/alerts/:alertId" element={<AlertDetailPage />} />
  </Routes></MemoryRouter></QueryClientProvider>)
}

describe('AlertDetailPage', () => {
  beforeEach(() => {
    requests = []
    detailResponse = alertDetailEnvelope
    revisionsResponse = alertRevisionsEnvelope
    revisionsStatus = 200
    detailStatus = 200
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      requests.push(url)
      if (url.pathname.endsWith('/revisions')) return Promise.resolve(new HttpResponse(revisionsResponse, { status: revisionsStatus }))
      return Promise.resolve(new HttpResponse(detailResponse, { status: detailStatus }))
    }))
  })

  it.each(['0', '-2', '2.5', 'abc', '9007199254740992'])('rejects invalid ID %s without making requests', (id) => {
    renderPage(id)
    expect(screen.getByRole('heading', { name: 'This alert ID is not valid.' })).toHaveFocus()
    expect(screen.getByText('Alert IDs must be positive whole numbers.')).toBeVisible()
    expect(requests).toHaveLength(0)
  })

  it('requests positive IDs, renders lifecycle detail, and orders the timeline ascending', async () => {
    revisionsResponse = { ...alertRevisionsEnvelope, data: [...alertRevisionsEnvelope.data].reverse() }
    renderPage('41')
    expect(await screen.findByRole('heading', { level: 1, name: 'Belgrave trains delayed' })).toHaveFocus()
    expect(requests.map((url) => url.pathname)).toEqual(expect.arrayContaining(['/api/v1/alerts/41', '/api/v1/alerts/41/revisions']))
    expect(screen.getByText('Historical', { selector: '.alert-feature__status' })).toBeVisible()
    expect(screen.getAllByText('Richmond').length).toBeGreaterThan(0)
    expect(screen.getByText(/currently installed GTFS schedule/)).toBeVisible()
    expect(screen.getByText('Source description').nextSibling).toHaveTextContent('Allow an extra 20 minutes')
    expect(screen.queryByRole('link', { name: 'Open source link' })).not.toBeInTheDocument()
    const revisions = screen.getAllByText(/Revision [23]/)
    expect(revisions[0]).toHaveTextContent('Revision 2')
    expect(revisions[1]).toHaveTextContent('Revision 3')
    expect(document.title).toBe('Alert 41 | Transit Observatory')
  })

  it('keeps detail visible when the independently loaded timeline fails', async () => {
    revisionsStatus = 400
    renderPage('41')
    expect(await screen.findByRole('heading', { level: 1, name: 'Belgrave trains delayed' })).toBeVisible()
    const error = await screen.findByRole('alert')
    expect(error).toHaveTextContent('Revision timeline could not be loaded')
    expect(screen.getByText('Historical', { selector: '.alert-feature__status' })).toBeVisible()
  })

  it('handles a deleted sparse latest revision and rejects unsafe source links', async () => {
    const sparse = {
      ...alertDetailEnvelope.data.latest_revision, header: [], description: [], url: [{ text: 'javascript:alert(1)' }],
      cause: undefined, effect: undefined, severity: undefined, routes: [], stations: [], active_periods: [], is_deleted: true,
    }
    detailResponse = { data: { ...alertDetailEnvelope.data, latest_revision: sparse } }
    revisionsResponse = { data: [sparse], meta: { count: 1 } }
    const user = userEvent.setup()
    renderPage('41')
    expect(await screen.findByText('Latest revision deleted')).toBeVisible()
    expect(screen.getByRole('heading', { level: 1, name: 'Alert 41' })).toBeVisible()
    expect(screen.getByText('No description supplied in the latest revision.')).toBeVisible()
    expect(screen.getAllByText('No stations listed').length).toBeGreaterThan(0)
    expect(screen.queryByRole('link', { name: 'Open source link' })).not.toBeInTheDocument()
    await user.click(screen.getByText('Revision content'))
    const revision = screen.getByText('Deletion observation').closest('article')
    expect(revision).not.toBeNull()
    expect(within(revision as HTMLElement).getByText('No description supplied in this revision.')).toBeVisible()
  })

  it('renders safe HTTP source links', async () => {
    detailResponse = { data: { ...alertDetailEnvelope.data, latest_revision: { ...alertDetailEnvelope.data.latest_revision, url: [{ text: 'https://transport.example/alert/41' }] } } }
    renderPage('41')
    expect(await screen.findByRole('link', { name: 'Open source link' })).toHaveAttribute('href', 'https://transport.example/alert/41')
  })

  it('presents a missing alert as not found rather than a retryable outage', async () => {
    detailStatus = 404
    detailResponse = { error: { code: 'not_found', message: 'alert not found' } }
    renderPage('41')
    expect(await screen.findByRole('heading', { name: 'Alert not found' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'Browse alert history' })).toHaveAttribute('href', '/alerts')
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()
  })
})
