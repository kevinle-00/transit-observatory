import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { currentEnvelope, lineEnvelope, richmondStation, stationDetailEnvelope, stationEnvelope, upcomingEnvelope } from '../fixtures/api'
import { HttpResponse } from '../test/http-response'
import { StationDetailPage } from './StationDetailPage'
import { StationExplorerPage } from './StationExplorerPage'

let requests: URL[]
let stationListResponse: unknown
let detailResponse: unknown

function LocationProbe() {
  return <output aria-label="location">{useLocation().search}</output>
}

function renderPage(element: React.ReactNode, entry: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retryDelay: 0, gcTime: Infinity } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[entry]}><Routes><Route path="*" element={<>{element}<LocationProbe /></>} /></Routes></MemoryRouter></QueryClientProvider>)
}

describe('station explorer routes', () => {
  beforeEach(() => {
    requests = []
    stationListResponse = stationEnvelope
    detailResponse = stationDetailEnvelope
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      requests.push(url)
      if (url.pathname.endsWith('/lines')) return Promise.resolve(new HttpResponse(lineEnvelope))
      if (url.pathname.includes('/stations/')) return Promise.resolve(new HttpResponse(detailResponse))
      if (url.pathname.endsWith('/stations')) return Promise.resolve(new HttpResponse(stationListResponse))
      return Promise.resolve(new HttpResponse(url.searchParams.get('status') === 'upcoming' ? upcomingEnvelope : currentEnvelope))
    }))
  })

  it('renders station facts, serving lines, counts, title, and focus', async () => {
    renderPage(<StationExplorerPage />, '/stations')
    const heading = await screen.findByRole('heading', { name: 'Richmond' })
    const card = heading.closest('article')
    expect(card).not.toBeNull()
    expect(within(card as HTMLElement).getByText('Belgrave')).toBeVisible()
    expect(within(card as HTMLElement).getByText('Current alerts')).toBeVisible()
    const link = within(card as HTMLElement).getByRole('link', { name: 'Station details' })
    expect(new URL(link.getAttribute('href') ?? '', 'http://localhost').searchParams.get('station_id')).toBe('station:richmond')
    expect(document.title).toBe('Stations | Transit Observatory')
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus()
  })

  it('submits literal URL-backed search without requesting per keystroke', async () => {
    const user = userEvent.setup()
    renderPage(<StationExplorerPage />, '/stations')
    await screen.findByRole('heading', { name: 'Richmond' })
    const beforeTyping = requests.filter((url) => url.pathname.endsWith('/stations')).length
    await user.type(screen.getByLabelText('Station name'), 'Richmond / East')
    expect(requests.filter((url) => url.pathname.endsWith('/stations'))).toHaveLength(beforeTyping)
    await user.click(screen.getByRole('button', { name: 'Search stations' }))
    await waitFor(() => expect(requests.some((url) => url.pathname.endsWith('/stations') && url.searchParams.get('q') === 'Richmond / East')).toBe(true))
    expect(screen.getByLabelText('location')).toHaveTextContent('q=Richmond+%2F+East')
    expect(screen.getByLabelText('Station name')).toHaveAttribute('maxlength', '100')
    await user.selectOptions(screen.getByLabelText('Serving line'), 'route:belgrave/1')
    await waitFor(() => expect(requests.some((url) => url.searchParams.get('line_id') === 'route:belgrave/1')).toBe(true))
  })

  it('shows empty and error results', async () => {
    stationListResponse = { data: [], meta: { count: 0 } }
    const { unmount } = renderPage(<StationExplorerPage />, '/stations?q=Nowhere')
    expect(await screen.findByRole('heading', { name: 'No stations found' })).toBeVisible()
    unmount()
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      if (url.pathname.endsWith('/stations')) return Promise.resolve(new HttpResponse({ error: { code: 'bad_request', message: 'No stations' } }, { status: 400 }))
      return Promise.resolve(new HttpResponse(lineEnvelope))
    })
    renderPage(<StationExplorerPage />, '/stations')
    expect(await screen.findByText('Stations could not be loaded')).toBeVisible()
  })

  it('keeps an encoded slash station ID exact and exposes detail associations', async () => {
    const rawId = 'station:richmond/platform/1'
    detailResponse = { data: { ...stationDetailEnvelope.data, station: { ...richmondStation, id: rawId } } }
    renderPage(<StationDetailPage />, `/stations/detail?station_id=${encodeURIComponent(rawId)}`)
    expect(await screen.findByRole('heading', { name: 'Richmond' })).toBeVisible()
    await waitFor(() => expect(requests.some((url) => url.pathname.endsWith('/stations/station%3Arichmond%2Fplatform%2F1'))).toBe(true))
    expect(requests.filter((url) => url.pathname.endsWith('/alerts')).every((url) => url.searchParams.get('station_id') === rawId)).toBe(true)
    const line = screen.getByRole('link', { name: 'Belgrave' })
    expect(new URL(line.getAttribute('href') ?? '', 'http://localhost').searchParams.get('line_id')).toBe('route:belgrave/1')
    expect(screen.getByText('Wheelchair boarding indicated')).toBeVisible()
    expect(screen.getByRole('link', { name: 'Belgrave trains delayed' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'Coaches replace trains on Sunday' })).toBeVisible()
    expect(screen.getByText(/platform-level impact/i)).toBeVisible()
    expect(document.title).toBe('Richmond | Transit Observatory')
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus()
  })
})
