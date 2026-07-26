import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { currentEnvelope, historicalEnvelope, lineAnalyticsEnvelope, lineDetailEnvelope, lineEnvelope, upcomingEnvelope } from '../fixtures/api'
import { HttpResponse } from '../test/http-response'
import { LineDetailPage } from './LineDetailPage'
import { LineExplorerPage } from './LineExplorerPage'

let requests: URL[]
let linesResponse: unknown

function response(url: URL): unknown {
  if (url.pathname.endsWith('/analytics/lines')) return lineAnalyticsEnvelope
  if (url.pathname.endsWith('/lines/route%3Abelgrave%2F1')) return lineDetailEnvelope
  if (url.pathname.endsWith('/lines')) return linesResponse
  if (url.searchParams.get('status') === 'historical') return { ...historicalEnvelope, meta: { ...historicalEnvelope.meta, page_size: 1, total_pages: 18 } }
  return url.searchParams.get('status') === 'upcoming' ? upcomingEnvelope : currentEnvelope
}

function renderPage(element: React.ReactNode, entry: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retryDelay: 0, gcTime: Infinity } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[entry]}><Routes><Route path="*" element={element} /></Routes></MemoryRouter></QueryClientProvider>)
}

describe('line explorer routes', () => {
  beforeEach(() => {
    requests = []
    linesResponse = lineEnvelope
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      requests.push(url)
      return Promise.resolve(new HttpResponse(response(url)))
    }))
  })

  it('renders official safe colors, counts, episode terminology, and raw-ID links', async () => {
    renderPage(<LineExplorerPage />, '/lines')
    const heading = await screen.findByRole('heading', { name: 'Belgrave' })
    const card = heading.closest('article')
    expect(card).not.toBeNull()
    expect(card).toHaveStyle('--explorer-accent: #006F45')
    expect(within(card as HTMLElement).getByText('Historical episodes, last 30 days')).toBeVisible()
    expect(within(card as HTMLElement).getByText('5')).toBeVisible()
    expect(within(card as HTMLElement).getByText('31')).toBeVisible()
    const detail = within(card as HTMLElement).getByRole('link', { name: 'Line details' })
    expect(new URL(detail.getAttribute('href') ?? '', 'http://localhost').searchParams.get('line_id')).toBe('route:belgrave/1')
    const analytics = within(card as HTMLElement).getByRole('link', { name: 'Episode analytics' })
    expect(new URL(analytics.getAttribute('href') ?? '', 'http://localhost').pathname).toBe('/analytics/line')
    expect(screen.getAllByRole('main')).toHaveLength(1)
    expect(document.title).toBe('Lines | Transit Observatory')
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus()
  })

  it('shows empty and independent error states', async () => {
    linesResponse = { data: [], meta: { count: 0 } }
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      if (url.pathname.endsWith('/analytics/lines')) return Promise.resolve(new HttpResponse({ error: { code: 'bad_request', message: 'No analytics' } }, { status: 400 }))
      return Promise.resolve(new HttpResponse(linesResponse))
    })
    renderPage(<LineExplorerPage />, '/lines')
    expect(await screen.findByRole('heading', { name: 'No rail lines available' })).toBeVisible()
    expect(await screen.findByText('Historical episodes are unavailable')).toBeVisible()
  })

  it('preserves colon and slash IDs in detail requests and renders strict associations', async () => {
    renderPage(<LineDetailPage />, '/lines/detail?line_id=route%3Abelgrave%2F1')
    expect(await screen.findByRole('heading', { name: 'Belgrave line' })).toBeVisible()
    await waitFor(() => expect(requests.some((url) => url.pathname.endsWith('/lines/route%3Abelgrave%2F1'))).toBe(true))
    expect(requests.filter((url) => url.pathname.endsWith('/alerts')).every((url) => url.searchParams.get('line_id') === 'route:belgrave/1')).toBe(true)
    expect(requests.some((url) => url.searchParams.get('status') === 'historical' && url.searchParams.get('page_size') === '1')).toBe(true)
    await waitFor(() => expect(screen.getByText('Lifecycle historical alerts').nextElementSibling).toHaveTextContent('18'))
    const station = screen.getByRole('link', { name: 'Richmond' })
    expect(new URL(station.getAttribute('href') ?? '', 'http://localhost').searchParams.get('station_id')).toBe('station:richmond')
    expect(screen.getByRole('link', { name: 'Belgrave trains delayed' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'Coaches replace trains on Sunday' })).toBeVisible()
    expect(screen.getByText(/no impact is inferred/i)).toBeVisible()
    expect(document.title).toBe('Belgrave line | Transit Observatory')
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus()
  })

  it('rejects an empty line identifier without making requests', () => {
    renderPage(<LineDetailPage />, '/lines/detail?line_id=')
    expect(screen.getByRole('heading', { name: 'A line is required' })).toBeVisible()
    expect(requests).toHaveLength(0)
  })
})
