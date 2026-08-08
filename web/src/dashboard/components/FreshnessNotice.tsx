import { CheckCircle2, CircleAlert } from 'lucide-react'
import type { Status } from '../../api/contracts'
import { formatFreshnessReason, formatMelbourneTime, formatOverallStatus } from '../../shared/format'

export function FreshnessNotice({ status }: { status: Status }) {
  const alerts = status.service_alerts
  const network = status.static_gtfs
  const isFresh = status.overall_status === 'ok' && alerts.freshness === 'fresh' && network.freshness === 'fresh' && alerts.reasons.length === 0 && network.reasons.length === 0
  if (isFresh) {
    return (
      <div className="freshness freshness--fresh" role="status" aria-label="Update status: up to date">
        <CheckCircle2 aria-hidden="true" />
        <div><strong>{formatOverallStatus(status.overall_status)}</strong><p>Service information is current.</p></div>
      </div>
    )
  }

  const title = formatOverallStatus(status.overall_status)
  return (
    <aside className="freshness freshness--warning" aria-label="Update status warning">
      <CircleAlert aria-hidden="true" />
      <div>
        <strong>{title}</strong>
        <p>
          Service information may have changed since the last successful update.
          This page shows the most recent data available.
        </p>
        <details>
          <summary>Update details</summary>
          <dl>
            <div><dt>Service alerts</dt><dd>{alerts.data_as_of ? `Updated ${formatMelbourneTime(alerts.data_as_of)}` : 'Update time unavailable'}</dd></div>
            <div><dt>Network schedule</dt><dd>{network.data_as_of ? `Updated ${formatMelbourneTime(network.data_as_of)}` : 'Update time unavailable'}</dd></div>
          </dl>
          {[...alerts.reasons, ...network.reasons].length > 0 && (
            <p>Reasons: {[...new Set([...alerts.reasons, ...network.reasons])].map(formatFreshnessReason).join(', ')}</p>
          )}
        </details>
      </div>
    </aside>
  )
}
