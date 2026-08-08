import { ChartNoAxesColumnIncreasing, Clock3, History, LayoutDashboard, MapPin, TrainFront } from 'lucide-react'
import { Link, NavLink } from 'react-router-dom'
import type { OverallStatus } from '../api/contracts'
import { formatMelbourneTime, formatOverallStatus } from '../shared/format'

interface AppHeaderProps {
  generatedAt?: string | undefined
  loading?: boolean
  status?: OverallStatus | undefined
}

export function AppHeader({ generatedAt, loading = false, status }: AppHeaderProps) {
  return (
    <header className="app-header">
      <Link className="app-brand" to="/" aria-label="Transit Observatory home">
        <span className="app-brand__mark"><TrainFront aria-hidden="true" /></span>
        <span><strong>Transit Observatory</strong><small>Melbourne Metro</small></span>
      </Link>
      <nav className="app-navigation" aria-label="Primary navigation">
        <NavLink to="/" end><LayoutDashboard aria-hidden="true" /><span>Overview</span></NavLink>
        <NavLink to="/lines"><TrainFront aria-hidden="true" /><span>Lines</span></NavLink>
        <NavLink to="/stations"><MapPin aria-hidden="true" /><span>Stations</span></NavLink>
        <NavLink to="/alerts"><History aria-hidden="true" /><span>Alert history</span></NavLink>
        <NavLink to="/analytics"><ChartNoAxesColumnIncreasing aria-hidden="true" /><span>Analytics</span></NavLink>
      </nav>
      {(generatedAt || loading || status) && (
        <p className="app-header__utility" role="status">
          <Clock3 aria-hidden="true" />
          <span>{generatedAt ? `Checked ${formatMelbourneTime(generatedAt)}` : loading ? 'Checking for updates' : 'Update time unavailable'}</span>
          {status && <strong className={`status-pill status-pill--${status}`}>{formatOverallStatus(status)}</strong>}
        </p>
      )}
    </header>
  )
}
