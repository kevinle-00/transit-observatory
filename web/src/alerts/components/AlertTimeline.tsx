import type { AlertRevision } from '../../api/contracts'
import { formatMelbourneTime, formatPeriods, humanize, selectTranslation } from '../../shared/format'
import { stationNames } from '../alert-utils'
import { AlertRouteBadges } from './AlertBadges'

export function AlertTimeline({ revisions }: { revisions: AlertRevision[] }) {
  const ordered = [...revisions].sort((left, right) => left.revision_number - right.revision_number)
  return (
    <ol className="alert-timeline">
      {ordered.map((revision) => {
        const headline = selectTranslation(revision.header)
        const description = selectTranslation(revision.description)
        const stations = stationNames(revision.stations)
        return (
          <li key={revision.revision_id}>
            <article>
              <header>
                <div><span>Revision {String(revision.revision_number)}</span><h3>{headline || (revision.is_deleted ? 'Deletion observation' : 'No headline supplied')}</h3></div>
                {revision.is_deleted && <strong className="alert-feature__deleted">Deleted</strong>}
              </header>
              <p className="alert-timeline__range">Observed {formatMelbourneTime(revision.revision_first_seen_at)} to {formatMelbourneTime(revision.revision_last_seen_at)}</p>
              <details>
                <summary>Revision content</summary>
                <div className="alert-timeline__content">
                  {description ? <p className="alert-feature__source-copy">{description}</p> : <p>No description supplied in this revision.</p>}
                  <dl className="alert-feature__facts">
                    <div><dt>Cause</dt><dd>{humanize(revision.cause)}</dd></div>
                    <div><dt>Effect</dt><dd>{humanize(revision.effect)}</dd></div>
                    <div><dt>Severity</dt><dd>{humanize(revision.severity)}</dd></div>
                    <div><dt>Source-listed stations</dt><dd>{stations.length ? stations.join(', ') : 'No stations listed'}</dd></div>
                  </dl>
                  <AlertRouteBadges routes={revision.routes} />
                  <ul className="alert-feature__periods">{formatPeriods(revision.active_periods).map((period, index) => <li key={`${String(index)}-${period}`}>{period}</li>)}</ul>
                </div>
              </details>
            </article>
          </li>
        )
      })}
    </ol>
  )
}
