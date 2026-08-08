import { useQuery } from '@tanstack/react-query'
import { Outlet } from 'react-router-dom'
import { statusQuery } from '../api/queries'
import { AppHeader } from './AppHeader'

export function AppShell() {
  const status = useQuery(statusQuery)

  return (
    <div className="app-canvas">
      <div className="app-frame">
        <a className="skip-link" href="#main">Skip to main content</a>
        <AppHeader
          generatedAt={status.data?.data.generated_at}
          loading={status.isPending}
          status={status.isError ? (status.data ? 'degraded' : 'unavailable') : status.data?.data.overall_status}
        />
        <Outlet />
        <footer><span>Transit Observatory</span><p>Melbourne Metro · Current service updates and alert history</p></footer>
      </div>
    </div>
  )
}
