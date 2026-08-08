import { useRef } from 'react'
import { Link, useRouteError } from 'react-router-dom'
import { AppHeader } from './AppHeader'
import { useRoutePage } from './use-route-page'

export function RouteError() {
  useRouteError()
  const heading = useRef<HTMLHeadingElement>(null)
  useRoutePage('Unexpected error | Transit Observatory', heading)

  return (
    <div className="app-canvas"><div className="app-frame app-frame--standalone">
      <a className="skip-link" href="#main">Skip to main content</a>
      <AppHeader />
      <main id="main" className="route-error">
        <p className="page-label">Unexpected error</p>
        <h1 ref={heading} tabIndex={-1}>We couldn't load this page.</h1>
        <p>Something went wrong. Try again or return to the network overview.</p>
        <div className="route-actions">
          <button type="button" onClick={() => window.location.reload()}>Retry this page</button>
          <Link to="/">Return to the network overview</Link>
        </div>
      </main>
    </div></div>
  )
}
