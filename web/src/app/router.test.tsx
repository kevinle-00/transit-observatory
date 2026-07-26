import { render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { NotFound } from './NotFound'
import { RouteError } from './RouteError'

function BrokenRoute(): never {
  throw new Error('private database connection detail')
}

describe('route states', () => {
  afterEach(() => vi.restoreAllMocks())

  it('presents and focuses the branded not-found route', async () => {
    const router = createMemoryRouter([{ path: '*', element: <NotFound /> }], { initialEntries: ['/missing'] })
    render(<RouterProvider router={router} />)
    const heading = await screen.findByRole('heading', { name: 'This line ends here.' })
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
    const heading = await screen.findByRole('heading', { name: 'The view could not be assembled.' })
    expect(document.title).toBe('Unexpected error | Transit Observatory')
    expect(heading).toHaveFocus()
    expect(screen.queryByText(/private database/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry this page' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'Return to the network overview' })).toBeVisible()
  })
})
