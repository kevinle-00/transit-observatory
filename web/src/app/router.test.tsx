import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { freshStatus } from '../fixtures/api'
import { HttpResponse } from '../test/http-response'
import { NotFound } from './NotFound'
import { RouteError } from './RouteError'
import { routes } from './router'

function BrokenRoute(): never {
  throw new Error('private database connection detail')
}

describe('route states', () => {
  afterEach(() => vi.restoreAllMocks())

  it('presents and focuses the branded not-found route', async () => {
    const router = createMemoryRouter([{ path: '*', element: <NotFound /> }], { initialEntries: ['/missing'] })
    render(<RouterProvider router={router} />)
    const heading = await screen.findByRole('heading', { name: "We couldn't find this page." })
    expect(document.title).toBe('Page not found | Transit Observatory')
    expect(heading).toHaveFocus()
    expect(screen.getByRole('link', { name: 'Return to the network overview' })).toHaveAttribute('href', '/')
  })

  it('contains unexpected route errors without leaking details', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const router = createMemoryRouter([{
      path: '/', element: <BrokenRoute />, errorElement: <RouteError />,
    }])
    render(<RouterProvider router={router} />)
    const heading = await screen.findByRole('heading', { name: "We couldn't load this page." })
    expect(document.title).toBe('Unexpected error | Transit Observatory')
    expect(heading).toHaveFocus()
    expect(screen.queryByText(/private database/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry this page' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'Return to the network overview' })).toBeVisible()
  })

  it('uses one real shell and marks navigation active for a direct detail bookmark', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new HttpResponse(freshStatus))))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
    const router = createMemoryRouter(routes, { initialEntries: ['/lines/detail'] })
    render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>)

    expect(await screen.findByRole('heading', { name: 'A line is required' })).toBeVisible()
    expect(screen.getAllByRole('main')).toHaveLength(1)
    expect(screen.getByRole('link', { name: 'Lines' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'Overview' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: 'Stations' })).toHaveAttribute('href', '/stations')
    expect(screen.getByRole('link', { name: 'Alert history' })).toHaveAttribute('href', '/alerts')
    expect(screen.getByRole('link', { name: 'Analytics' })).toHaveAttribute('href', '/analytics')
    expect(await screen.findByText('Up to date', { selector: '.status-pill' })).toBeVisible()
  })
})
