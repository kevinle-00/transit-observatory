import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import type { Alert } from '../api/contracts'
import { alertListQuery, stationDetailQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { cssColor, formatCount, selectTranslation } from '../shared/format'
import { ErrorState, LoadingState } from '../shared/QueryState'
import { isNotFoundError } from '../shared/api-errors'
import { ResourceNotFound } from '../shared/ResourceNotFound'
import { isValidResourceId } from '../shared/resource-id'

function queryLink(pathname: string, key: string, value: string | number) {
  return { pathname, search: new URLSearchParams({ [key]: String(value) }).toString() }
}

function wheelchairLabel(value: number | undefined) {
  if (value === 1) return 'Wheelchair boarding indicated'
  if (value === 2) return 'Wheelchair boarding not available'
  return 'No wheelchair boarding information supplied'
}

function AlertList({ alerts, empty }: { alerts: Alert[]; empty: string }) {
  if (alerts.length === 0) return <p className="explorer-empty-inline">{empty}</p>
  return <ul className="explorer-link-list">{alerts.map((alert) => <li key={alert.id}><Link to={`/alerts/${String(alert.id)}`}>{selectTranslation(alert.header) ?? `Alert ${String(alert.id)}`}</Link></li>)}</ul>
}

export function StationDetailPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  const [params] = useSearchParams()
  const stationId = params.get('station_id')
  const valid = isValidResourceId(stationId)
  const detail = useQuery({ ...stationDetailQuery(stationId ?? ''), enabled: valid })
  const current = useQuery({ ...alertListQuery({ status: 'current', stationId: stationId ?? '' }), enabled: valid })
  const upcoming = useQuery({ ...alertListQuery({ status: 'upcoming', stationId: stationId ?? '' }), enabled: valid })
  const detailNotFound = isNotFoundError(detail.error)
  const station = detail.data?.data.station
  useRoutePage(`${station?.name ?? 'Station details'} | Transit Observatory`, heading)

  return (
    <main id="main" className="workspace feature-page station-detail">
      <header className="explorer-intro">
        <Link className="explorer-back" to="/stations">All stations</Link>
        <h1 ref={heading} tabIndex={-1}>{station?.name ?? 'Station details'}</h1>
        {station && <p className="explorer-identity"><code>{station.id}</code></p>}
      </header>

      {!valid && <section className="explorer-empty" role="alert"><h2>A station is required</h2><p>Choose a station from the station explorer to view its details.</p></section>}
      {valid && detail.isPending && <LoadingState label="Loading station details" />}
      {detailNotFound && <ResourceNotFound resource="Station" backTo="/stations" backLabel="Return to station search" />}
      {detail.error && !detail.data && !detailNotFound && <ErrorState title="Station details could not be loaded" message={`No details are available for ${stationId ?? 'this station'}.`} onRetry={() => void detail.refetch()} />}
      {detail.error && detail.data && !detailNotFound && <ErrorState cached title="Could not refresh station details" message="Showing the last available station information." onRetry={() => void detail.refetch()} />}

      {station && !detailNotFound && (
        <>
          <dl className="explorer-facts">
            <div><dt>Latitude</dt><dd>{station.latitude ?? 'Not supplied'}</dd></div>
            <div><dt>Longitude</dt><dd>{station.longitude ?? 'Not supplied'}</dd></div>
            <div><dt>Accessibility</dt><dd>{wheelchairLabel(station.wheelchair_boarding)}</dd></div>
            <div><dt>Current alerts</dt><dd>{formatCount(current.data?.meta.count ?? station.current_alert_count)}</dd></div>
            <div><dt>Upcoming alerts</dt><dd>{formatCount(upcoming.data?.meta.count ?? station.upcoming_alert_count)}</dd></div>
          </dl>
          <nav className="explorer-actions explorer-actions--page" aria-label="Station pages">
            <Link to="/">Current service</Link>
            <Link to={queryLink('/alerts', 'station_id', station.id)}>Alert history</Link>
          </nav>
          <section className="explorer-panel" aria-labelledby="serving-lines-title"><h2 id="serving-lines-title">Serving lines</h2>
            {station.lines.length === 0 ? <p className="explorer-empty-inline">No serving lines are listed.</p> : <ul className="explorer-link-list">{station.lines.map((line) => <li key={line.id} style={{ '--explorer-accent': cssColor(line.color) } as React.CSSProperties}><span className="explorer-swatch" aria-hidden="true" /><Link to={queryLink('/lines/detail', 'line_id', line.id)}>{line.long_name || line.short_name}</Link></li>)}</ul>}
          </section>
          <div className="explorer-columns">
            <section className="explorer-panel" aria-labelledby="station-current-title"><h2 id="station-current-title">Current alerts</h2>
              {current.isPending && <LoadingState compact label="Loading current alerts" />}
              {current.error && !current.data && <ErrorState title="Current alerts unavailable" message="This classification could not be loaded." onRetry={() => void current.refetch()} />}
              {current.error && current.data && <ErrorState cached title="Could not refresh current alerts" message="Showing the last available results." onRetry={() => void current.refetch()} />}
              {current.data && <AlertList alerts={current.data.data} empty="No current alerts associated with this station." />}
            </section>
            <section className="explorer-panel" aria-labelledby="station-upcoming-title"><h2 id="station-upcoming-title">Upcoming alerts</h2>
              {upcoming.isPending && <LoadingState compact label="Loading upcoming alerts" />}
              {upcoming.error && !upcoming.data && <ErrorState title="Upcoming alerts unavailable" message="This classification could not be loaded." onRetry={() => void upcoming.refetch()} />}
              {upcoming.error && upcoming.data && <ErrorState cached title="Could not refresh upcoming alerts" message="Showing the last available results." onRetry={() => void upcoming.refetch()} />}
              {upcoming.data && <AlertList alerts={upcoming.data.data} empty="No upcoming alerts associated with this station." />}
            </section>
          </div>
          <p className="explorer-caveat">Station impact is inferred only from source-listed station and route associations matched by the backend. An association does not confirm platform-level impact or that every affected service is listed.</p>
        </>
      )}
    </main>
  )
}
