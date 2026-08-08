import type { ActivePeriod, Alert, AlertRoute } from '../../api/contracts'
import { Link } from 'react-router-dom'
import { cssColor, formatMelbourneTime, formatPeriods, humanize, selectTranslation } from '../../shared/format'

function routeName(route: AlertRoute) {
  return route.short_name || route.long_name || route.source_route_id
}

function timestamp(value: string | undefined) {
  if (!value) return null
  const parsed = new Date(value).getTime()
  return Number.isNaN(parsed) ? null : parsed
}

function primaryPeriod(periods: ActivePeriod[], kind: 'current' | 'upcoming') {
  const now = Date.now()
  const candidates = periods.map((period, index) => ({ period, index, start: timestamp(period.starts_at), end: timestamp(period.ends_at) }))
  if (kind === 'current') {
    return candidates.find(({ start, end }) => (start === null || start <= now) && (end === null || end >= now)) ?? candidates[0]
  }
  return candidates
    .filter(({ start }) => start !== null && start > now)
    .sort((left, right) => (left.start ?? Infinity) - (right.start ?? Infinity))[0] ?? candidates[0]
}

export function AlertArticle({ alert, kind }: { alert: Alert; kind: 'current' | 'upcoming' }) {
  const headline = selectTranslation(alert.header) || 'Service update'
  const description = selectTranslation(alert.description)
  const unmatchedRoutes = alert.routes.filter((route) => !route.is_matched)
  const unmatchedStations = alert.stations.filter((station) => !station.is_matched)
  const namedStations = alert.stations.map((station) => station.name || station.source_stop_id)
  const selectedPeriod = primaryPeriod(alert.active_periods, kind)
  const primaryTiming = selectedPeriod ? formatPeriods([selectedPeriod.period])[0] : formatPeriods([])[0]
  const remainingPeriods = alert.active_periods.filter((_, index) => index !== selectedPeriod?.index)
  const additionalPeriods = remainingPeriods.length > 0 ? formatPeriods(remainingPeriods) : []

  return (
    <article id={`alert-${String(alert.id)}`} className="alert-card" tabIndex={-1}>
      <div className="alert-card__meta">
        <div className="line-badges" aria-label="Affected lines">
          {alert.routes.length === 0 && <span className="line-badge line-badge--neutral">Network notice</span>}
          {alert.routes.map((route) => (
            <span className="line-badge" key={route.source_route_id} style={{ '--badge-color': cssColor(route.color) } as React.CSSProperties}>
              <i aria-hidden="true" />{routeName(route)}{!route.is_matched && <em>not in current schedule</em>}
            </span>
          ))}
        </div>
        {alert.severity && <span className="severity-badge">{humanize(alert.severity)}</span>}
      </div>
      <h3><Link to={`/alerts/${String(alert.id)}`}>{headline}</Link></h3>
      <dl className="alert-facts">
        <div><dt>Timing</dt><dd>{primaryTiming}</dd></div>
        <div><dt>Cause / effect</dt><dd>{humanize(alert.cause)} · {humanize(alert.effect)}</dd></div>
      </dl>
      {(description || namedStations.length > 0 || additionalPeriods.length > 0 || unmatchedRoutes.length > 0 || unmatchedStations.length > 0) && (
        <details className="alert-details">
          <summary>More details</summary>
          <div className="alert-details__content">
            {description && <div className="alert-card__description"><strong>Service update</strong><p>{description}</p></div>}
            <dl>
              <div><dt>Stations</dt><dd>{namedStations.length > 0 ? namedStations.join(', ') : 'No specific stations listed'}</dd></div>
              {additionalPeriods.length > 0 && <div><dt>Additional periods</dt><dd>{additionalPeriods.map((period) => <span key={period}>{period}</span>)}</dd></div>}
              <div><dt>Last seen</dt><dd>{formatMelbourneTime(alert.revision_last_seen_at)}</dd></div>
            </dl>
            {(unmatchedRoutes.length > 0 || unmatchedStations.length > 0) && (
              <p className="match-note"><strong>Not in current schedule:</strong> {[
                ...unmatchedRoutes.map((route) => `line ${route.source_route_id}`),
                ...unmatchedStations.map((station) => `station ${station.source_stop_id}`),
              ].join(', ')}.</p>
            )}
          </div>
        </details>
      )}
    </article>
  )
}
