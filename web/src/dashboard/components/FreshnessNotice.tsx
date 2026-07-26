import { CheckCircle2, CircleAlert } from 'lucide-react'
import type { Status } from '../../api/contracts'
import { formatMelbourneTime, formatOverallStatus, humanize } from '../../shared/format'

export function FreshnessNotice({ status }: { status: Status }) {
  const alerts = status.service_alerts
  const network = status.static_gtfs
  const isFresh = status.overall_status === 'ok' && alerts.freshness === 'fresh' && network.freshness === 'fresh' && alerts.reasons.length === 0 && network.reasons.length === 0
  if (isFresh) {
    return (
      <div className="freshness freshness--fresh" role="status" aria-label="Data freshness: fresh">
        <CheckCircle2 aria-hidden="true" />
        <div><strong>{formatOverallStatus(status.overall_status)}</strong><p>Latest accepted service information is current.</p></div>
      </div>
    )
  }

  const title = formatOverallStatus(status.overall_status)
  return (
    <aside className="freshness freshness--warning" aria-label="Data freshness warning">
      <CircleAlert aria-hidden="true" />
      <div>
        <strong>{title}</strong>
        <p>
          Some service information may have changed since the latest successful update.
          Displayed classifications come from the observatory’s latest accepted data.
        </p>
        <details>
          <summary>Source checks and reasons</summary>
          <dl>
            <div><dt>Service alerts</dt><dd>{alerts.data_as_of ? `Data as of ${formatMelbourneTime(alerts.data_as_of)}` : 'No accepted data timestamp'}</dd></div>
            <div><dt>Network schedule</dt><dd>{network.data_as_of ? `Data as of ${formatMelbourneTime(network.data_as_of)}` : 'No accepted data timestamp'}</dd></div>
          </dl>
          {[...alerts.reasons, ...network.reasons].length > 0 && (
            <p>Reasons: {[...new Set([...alerts.reasons, ...network.reasons])].map(humanize).join(', ')}</p>
          )}
        </details>
      </div>
    </aside>
  )
}
