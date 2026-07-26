import { useRef } from 'react'
import { Link } from 'react-router-dom'
import { AppHeader } from './AppHeader'
import { useRoutePage } from './use-route-page'

export function NotFound() {
  const heading = useRef<HTMLHeadingElement>(null)
  useRoutePage('Page not found | Transit Observatory', heading)

  return (
    <div className="app-canvas"><div className="app-frame app-frame--standalone">
      <a className="skip-link" href="#main">Skip to main content</a>
      <AppHeader />
      <main id="main" className="not-found">
        <p className="page-label">Page not found</p>
        <h1 ref={heading} tabIndex={-1}>This line ends here.</h1>
        <p>The page you requested is not part of the observatory.</p>
        <Link to="/">Return to the network overview</Link>
      </main>
    </div></div>
  )
}
