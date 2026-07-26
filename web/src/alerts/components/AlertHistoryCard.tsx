import { Link } from 'react-router-dom'
import type { Alert } from '../../api/contracts'
import { formatMelbourneTime, humanize, selectTranslation } from '../../shared/format'
import { stationNames } from '../alert-utils'
import { AlertRouteBadges } from './AlertBadges'

export function AlertHistoryCard({ alert }: { alert: Alert }) {
  const headline = selectTranslation(alert.header) || 'Service update'
  const stations = stationNames(alert.stations)

  return (
    <article className="alert-history-card">
      <div className="alert-history-card__top">
        <AlertRouteBadges routes={alert.routes} />
        {alert.severity && <span className="alert-feature__severity">{humanize(alert.severity)}</span>}
      </div>
      <h2><Link to={`/alerts/${String(alert.id)}`}>{headline}</Link></h2>
      <dl className="alert-feature__facts">
        <div><dt>Displayed revision last observed</dt><dd>{formatMelbourneTime(alert.revision_last_seen_at)}</dd></div>
        <div><dt>Cause / effect</dt><dd>{humanize(alert.cause)} / {humanize(alert.effect)}</dd></div>
        <div><dt>Source-listed stations</dt><dd>{stations.length ? stations.join(', ') : 'No stations listed'}</dd></div>
      </dl>
      <Link className="alert-feature__action" to={`/alerts/${String(alert.id)}`} aria-label={`View alert history: ${headline}`}>View lifecycle</Link>
    </article>
  )
}
