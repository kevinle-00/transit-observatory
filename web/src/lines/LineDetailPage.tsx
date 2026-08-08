import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import type { Alert } from '../api/contracts'
import { alertListQuery, historicalAlertsQuery, lineDetailQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { cssColor, formatCount, selectTranslation } from '../shared/format'
import { ErrorState, LoadingState } from '../shared/QueryState'
import { isNotFoundError } from '../shared/api-errors'
import { ResourceNotFound } from '../shared/ResourceNotFound'
import { isValidResourceId } from '../shared/resource-id'

function searchLink(pathname: string, key: string, value: string | number) {
  return { pathname, search: new URLSearchParams({ [key]: String(value) }).toString() }
}

function AlertLinks({ alerts, empty }: { alerts: Alert[]; empty: string }) {
  if (alerts.length === 0) return <p className="explorer-empty-inline">{empty}</p>
  return <ul className="explorer-link-list">{alerts.map((alert) => (
    <li key={alert.id}><Link to={`/alerts/${String(alert.id)}`}>{selectTranslation(alert.header) ?? `Alert ${String(alert.id)}`}</Link></li>
  ))}</ul>
}

export function LineDetailPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  const [params] = useSearchParams()
  const lineId = params.get('line_id')
  const valid = isValidResourceId(lineId)
  const detail = useQuery({ ...lineDetailQuery(lineId ?? ''), enabled: valid })
  const current = useQuery({ ...alertListQuery({ status: 'current', lineId: lineId ?? '' }), enabled: valid })
  const upcoming = useQuery({ ...alertListQuery({ status: 'upcoming', lineId: lineId ?? '' }), enabled: valid })
  const historical = useQuery({ ...historicalAlertsQuery({ lineId: lineId ?? '', pageSize: 1 }), enabled: valid })
  const detailNotFound = isNotFoundError(detail.error)
  const line = detail.data?.data.line
  const title = line ? `${line.long_name || line.short_name} line` : 'Line details'
  useRoutePage(`${title} | Transit Observatory`, heading)

  return (
    <main id="main" className="workspace feature-page line-detail">
      <header className="explorer-intro">
        <Link className="explorer-back" to="/lines">All lines</Link>
        <h1 ref={heading} tabIndex={-1}>{title}</h1>
        {line && <p className="explorer-identity" style={{ '--explorer-accent': cssColor(line.color) } as React.CSSProperties}><span className="explorer-swatch" aria-hidden="true" />{line.short_name} <code>{line.id}</code></p>}
      </header>

      {!valid && <section className="explorer-empty" role="alert"><h2>A line is required</h2><p>Choose a line from the line explorer to view its details.</p></section>}
      {valid && detail.isPending && <LoadingState label="Loading line details" />}
      {detailNotFound && <ResourceNotFound resource="Line" backTo="/lines" backLabel="Return to all lines" />}
      {detail.error && !detail.data && !detailNotFound && <ErrorState title="Line details could not be loaded" message={`No details are available for ${lineId ?? 'this line'}.`} onRetry={() => void detail.refetch()} />}
      {detail.error && detail.data && !detailNotFound && <ErrorState cached title="Could not refresh line details" message="Showing the last available line and station information." onRetry={() => void detail.refetch()} />}

      {line && detail.data && !detailNotFound && (
        <>
          <dl className="explorer-facts">
            <div><dt>Stations</dt><dd>{formatCount(line.station_count)}</dd></div>
            <div><dt>Current alerts</dt><dd>{formatCount(current.data?.meta.count ?? line.current_alert_count)}</dd></div>
            <div><dt>Upcoming alerts</dt><dd>{formatCount(upcoming.data?.meta.count ?? line.upcoming_alert_count)}</dd></div>
            <div><dt>Past alerts</dt><dd>{historical.data ? formatCount(historical.data.meta.total) : historical.isError ? 'Unavailable' : 'Loading'}</dd></div>
          </dl>
          <nav className="explorer-actions explorer-actions--page" aria-label="Line pages">
            <Link to={searchLink('/', 'line_id', line.id)}>Current service</Link>
            <Link to={searchLink('/alerts', 'line_id', line.id)}>Alert history</Link>
            <Link to={searchLink('/analytics/line', 'line_id', line.id)}>Line analytics</Link>
          </nav>
          <section className="explorer-panel" aria-labelledby="line-stations-title">
            <h2 id="line-stations-title">Stations</h2>
            {detail.data.data.stations.length === 0 ? <p className="explorer-empty-inline">No stations are listed for this line.</p> : (
              <ul className="explorer-link-list">{detail.data.data.stations.map((station) => <li key={station.id}><Link to={searchLink('/stations/detail', 'station_id', station.id)}>{station.name}</Link></li>)}</ul>
            )}
          </section>
          <div className="explorer-columns">
            <section className="explorer-panel" aria-labelledby="line-current-title"><h2 id="line-current-title">Current alerts</h2>
              {current.isPending && <LoadingState compact label="Loading current alerts" />}
              {current.error && !current.data && <ErrorState title="Current alerts unavailable" message="We couldn't load current alerts for this line." onRetry={() => void current.refetch()} />}
              {current.error && current.data && <ErrorState cached title="Could not refresh current alerts" message="Showing the last available results." onRetry={() => void current.refetch()} />}
              {current.data && <AlertLinks alerts={current.data.data} empty="No current alerts for this line." />}
            </section>
            <section className="explorer-panel" aria-labelledby="line-upcoming-title"><h2 id="line-upcoming-title">Upcoming alerts</h2>
              {upcoming.isPending && <LoadingState compact label="Loading upcoming alerts" />}
              {upcoming.error && !upcoming.data && <ErrorState title="Upcoming alerts unavailable" message="We couldn't load upcoming alerts for this line." onRetry={() => void upcoming.refetch()} />}
              {upcoming.error && upcoming.data && <ErrorState cached title="Could not refresh upcoming alerts" message="Showing the last available results." onRetry={() => void upcoming.refetch()} />}
              {upcoming.data && <AlertLinks alerts={upcoming.data.data} empty="No upcoming alerts for this line." />}
            </section>
          </div>
          {historical.error && !historical.data && <ErrorState title="Past alert count unavailable" message="Stations and current alerts are still available." onRetry={() => void historical.refetch()} />}
          {historical.error && historical.data && <ErrorState cached title="Could not refresh the past alert count" message="Showing the last available count." onRetry={() => void historical.refetch()} />}
          <p className="explorer-caveat">Affected lines and stations come from service updates and may not include every affected service.</p>
        </>
      )}
    </main>
  )
}
