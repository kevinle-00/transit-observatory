import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { alertDetailQuery, alertRevisionsQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { formatMelbourneTime, formatPeriods, humanize, selectTranslation } from '../shared/format'
import { ErrorState, LoadingState } from '../shared/QueryState'
import { isNotFoundError } from '../shared/api-errors'
import { ResourceNotFound } from '../shared/ResourceNotFound'
import { stationNames } from './alert-utils'
import { AlertRouteBadges } from './components/AlertBadges'
import { AlertTimeline } from './components/AlertTimeline'
import '../alerts.css'

function positiveInteger(value: string | undefined) {
  if (!value || !/^[1-9]\d*$/.test(value)) return null
  const number = Number(value)
  return Number.isSafeInteger(number) ? number : null
}

function safeHttpUrl(value: string | null) {
  if (!value) return null
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : null
  } catch {
    return null
  }
}

function sourceHost(value: string) {
  try { return new URL(value).hostname }
  catch { return 'Source feed not available' }
}

export function AlertDetailPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  const id = positiveInteger(useParams().alertId)
  useRoutePage(id ? `Alert ${String(id)} | Transit Observatory` : 'Invalid alert | Transit Observatory', heading)
  const detail = useQuery({ ...alertDetailQuery(id ?? 1), enabled: id !== null })
  const revisions = useQuery({ ...alertRevisionsQuery(id ?? 1), enabled: id !== null })
  const detailNotFound = isNotFoundError(detail.error)

  if (id === null) return (
    <main id="main" className="workspace feature-page alert-detail-page">
      <span className="page-label">Invalid alert</span><h1 ref={heading} tabIndex={-1}>This alert ID is not valid.</h1>
      <p className="alert-detail-page__invalid">Alert IDs must be positive whole numbers.</p><Link className="alert-feature__action" to="/alerts">Browse alert history</Link>
    </main>
  )

  const lifecycle = detail.data?.data
  const latest = lifecycle?.latest_revision
  const headline = latest ? selectTranslation(latest.header) : null
  const description = latest ? selectTranslation(latest.description) : null
  const sourceLink = latest ? safeHttpUrl(selectTranslation(latest.url)) : null
  const stations = latest ? stationNames(latest.stations) : []

  return (
    <main id="main" className="workspace feature-page alert-detail-page">
      <p className="alert-feature__back"><Link to="/">Network overview</Link> · <Link to="/alerts">Alert history</Link> · Alert {String(id)}</p>
      <section className="alert-feature__intro" aria-labelledby="alert-detail-title">
        <div><span>{lifecycle?.status === 'present' ? 'Present lifecycle' : lifecycle?.status === 'historical' ? 'Historical lifecycle' : 'Alert lifecycle'}</span><h1 id="alert-detail-title" ref={heading} tabIndex={-1}>{headline || `Alert ${String(id)}`}</h1></div>
        <p>This record follows one source alert across accepted feed observations. Source-listed lines and stations are explicit feed references, not inferred passenger impact.</p>
      </section>
      <aside className="alert-feature__limitation">Matches use the currently installed GTFS schedule, not necessarily the schedule in effect when a historical revision was observed. Unmatched source references remain visible.</aside>

      {detail.isPending && <LoadingState label="Loading alert lifecycle" />}
      {detailNotFound && <ResourceNotFound resource="Alert" backTo="/alerts" backLabel="Browse alert history" />}
      {detail.error && !detail.data && !detailNotFound && <ErrorState title="Alert detail could not be loaded" message="The lifecycle record is temporarily unavailable." onRetry={() => void detail.refetch()} />}
      {detail.error && detail.data && !detailNotFound && <ErrorState cached title="Could not refresh alert detail" message="Showing the last available lifecycle record." onRetry={() => void detail.refetch()} />}
      {lifecycle && latest && !detailNotFound && <article className="alert-detail-summary">
        <header><div><span className={`alert-feature__status alert-feature__status--${lifecycle.status}`}>{humanize(lifecycle.status)}</span>{latest.is_deleted && <span className="alert-feature__deleted">Latest revision deleted</span>}</div><strong>{String(lifecycle.revision_count)} revisions</strong></header>
        <AlertRouteBadges routes={latest.routes} />
        <dl className="alert-feature__facts alert-detail-summary__lifecycle">
          <div><dt>First observed</dt><dd>{formatMelbourneTime(lifecycle.first_seen_at)}</dd></div>
          <div><dt>Last observed</dt><dd>{formatMelbourneTime(lifecycle.last_seen_at)}</dd></div>
          <div><dt>Closed</dt><dd>{lifecycle.closed_at ? formatMelbourneTime(lifecycle.closed_at) : 'Not closed'}</dd></div>
          <div><dt>Source entity</dt><dd><code>{lifecycle.source_entity_id}</code></dd></div>
          <div><dt>Source feed</dt><dd>{sourceHost(lifecycle.source_url)}</dd></div>
          <div><dt>Cause</dt><dd>{humanize(latest.cause)}</dd></div>
          <div><dt>Effect</dt><dd>{humanize(latest.effect)}</dd></div>
          <div><dt>Severity</dt><dd>{humanize(latest.severity)}</dd></div>
          <div><dt>Source-listed stations</dt><dd>{stations.length ? stations.join(', ') : 'No stations listed'}</dd></div>
        </dl>
        <section className="alert-detail-summary__description" aria-labelledby="source-description-title"><h2 id="source-description-title">Source description</h2>{description ? <p className="alert-feature__source-copy">{description}</p> : <p>No description supplied in the latest revision.</p>}{sourceLink && <a href={sourceLink} target="_blank" rel="noreferrer">Open source link</a>}</section>
        <section aria-labelledby="active-periods-title"><h2 id="active-periods-title">All active periods</h2><ul className="alert-feature__periods">{formatPeriods(latest.active_periods).map((period, index) => <li key={`${String(index)}-${period}`}>{period}</li>)}</ul></section>
      </article>}

      {!detailNotFound && <section className="alert-revisions" aria-labelledby="alert-revisions-title">
        <header><span>Observation record</span><h2 id="alert-revisions-title">Revision timeline</h2></header>
        {revisions.isPending && <LoadingState label="Loading revision timeline" />}
        {revisions.error && !revisions.data && <ErrorState title="Revision timeline could not be loaded" message="Lifecycle detail remains available above." onRetry={() => void revisions.refetch()} />}
        {revisions.error && revisions.data && <ErrorState cached title="Could not refresh revision timeline" message="Showing the last available revisions." onRetry={() => void revisions.refetch()} />}
        {revisions.data?.data.length === 0 && <div className="empty-state"><strong>No revisions available</strong></div>}
        {revisions.data && revisions.data.data.length > 0 && <AlertTimeline revisions={revisions.data.data} />}
      </section>}
    </main>
  )
}
