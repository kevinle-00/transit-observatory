import { Clock3, TrainFront } from 'lucide-react'
import { Link } from 'react-router-dom'
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
      {(generatedAt || loading || status) && (
        <p className="app-header__utility" role="status">
          <Clock3 aria-hidden="true" />
          <span>{generatedAt ? `Generated ${formatMelbourneTime(generatedAt)}` : loading ? 'Checking latest data' : 'Update time unavailable'}</span>
          {status && <strong className={`status-pill status-pill--${status}`}>{formatOverallStatus(status)}</strong>}
        </p>
      )}
    </header>
  )
}
