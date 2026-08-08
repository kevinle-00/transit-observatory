import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { lineAnalyticsQuery, linesQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { cssColor, formatCount } from '../shared/format'
import { ErrorState, LoadingState } from '../shared/QueryState'

function lineSearch(pathname: string, lineId: string) {
  return { pathname, search: new URLSearchParams({ line_id: lineId }).toString() }
}

export function LineExplorerPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  useRoutePage('Lines | Transit Observatory', heading)
  const lines = useQuery(linesQuery)
  const analytics = useQuery(lineAnalyticsQuery())
  const episodesByLine = new Map(analytics.data?.data.map((item) => [
    item.line.id,
    item.series.reduce((total, point) => total + point.alert_count, 0),
  ]))

  return (
    <main id="main" className="workspace feature-page line-explorer">
      <header className="explorer-intro">
        <p className="explorer-kicker">Network directory</p>
        <h1 ref={heading} tabIndex={-1}>Rail lines</h1>
        <p>Explore stations, current service alerts, and recent alert history for each line.</p>
      </header>

      {analytics.isPending && <LoadingState compact label="Loading past alert counts" />}
      {analytics.error && !analytics.data && <ErrorState title="Past alert counts are unavailable" message="Lines and current alerts can still be viewed." onRetry={() => void analytics.refetch()} />}
      {analytics.error && analytics.data && <ErrorState cached title="Could not refresh past alert counts" message="Showing the last available 30-day counts." onRetry={() => void analytics.refetch()} />}

      {lines.isPending && <LoadingState label="Loading rail lines" />}
      {lines.error && !lines.data && <ErrorState title="Rail lines could not be loaded" message="The line directory is currently unavailable." onRetry={() => void lines.refetch()} />}
      {lines.error && lines.data && <ErrorState cached title="Could not refresh rail lines" message="Showing the last available line directory." onRetry={() => void lines.refetch()} />}
      {lines.data?.data.length === 0 && (
        <section className="explorer-empty" aria-labelledby="empty-lines-title">
          <h2 id="empty-lines-title">No rail lines available</h2>
          <p>No rail lines are available in the current schedule.</p>
        </section>
      )}
      {lines.data && lines.data.data.length > 0 && (
        <ul className="explorer-grid" aria-label="Rail lines">
          {lines.data.data.map((line) => {
            const episodeCount = episodesByLine.get(line.id)
            return (
              <li key={line.id}>
                <article className="explorer-card" style={{ '--explorer-accent': cssColor(line.color) } as React.CSSProperties}>
                  <div className="explorer-card__title">
                    <span className="explorer-swatch" aria-hidden="true" />
                    <div><h2>{line.long_name || line.short_name}</h2>{line.short_name !== line.long_name && <p>{line.short_name}</p>}</div>
                  </div>
                  <dl className="explorer-facts explorer-facts--compact">
                    <div><dt>Stations</dt><dd>{formatCount(line.station_count)}</dd></div>
                    <div><dt>Current alerts</dt><dd>{formatCount(line.current_alert_count)}</dd></div>
                    <div><dt>Upcoming alerts</dt><dd>{formatCount(line.upcoming_alert_count)}</dd></div>
                    <div><dt>Alerts recorded, last 30 days</dt><dd>{episodeCount === undefined ? 'Not available' : formatCount(episodeCount)}</dd></div>
                  </dl>
                  <nav className="explorer-actions" aria-label={`${line.long_name || line.short_name} links`}>
                    <Link to={lineSearch('/lines/detail', line.id)}>Line details</Link>
                    <Link to={lineSearch('/analytics/line', line.id)}>Alert analytics</Link>
                  </nav>
                </article>
              </li>
            )
          })}
        </ul>
      )}
      <p className="explorer-caveat">Past counts show recorded alerts, not incidents or affected passengers. Alerts are grouped by when they apply; we do not label them planned or unplanned.</p>
    </main>
  )
}
