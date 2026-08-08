import { useEffect, useRef, useState } from 'react'
import type { Alert } from '../../api/contracts'
import { ErrorState, LoadingState } from '../../shared/QueryState'
import { AlertArticle } from './AlertArticle'

interface AlertSectionProps {
  kind: 'current' | 'upcoming'
  alerts?: Alert[] | undefined
  count?: number | undefined
  error: boolean
  loading: boolean
  cached: boolean
  preview: boolean
  onRetry: () => void
  onViewAll: () => void
}

export function AlertSection({ kind, alerts, count, error, loading, cached, preview, onRetry, onViewAll }: AlertSectionProps) {
  const [visibleCount, setVisibleCount] = useState(preview ? 5 : 20)
  const focusAlertId = useRef<number | null>(null)
  const title = kind === 'current' ? 'Current alerts' : 'Upcoming alerts'
  const deck = kind === 'current'
    ? 'Service changes active now.'
    : 'Service changes scheduled for later.'
  const visibleAlerts = alerts?.slice(0, visibleCount)
  const hasMore = !preview && Boolean(alerts && visibleCount < alerts.length)
  const hasPreviewOverflow = preview && Boolean(alerts && alerts.length > 5)

  useEffect(() => {
    if (focusAlertId.current === null) return
    document.getElementById(`alert-${String(focusAlertId.current)}`)?.focus()
    focusAlertId.current = null
  }, [visibleCount])

  const showMore = () => {
    if (!alerts) return
    focusAlertId.current = alerts[visibleCount]?.id ?? null
    setVisibleCount((value) => value + 20)
  }

  return (
    <section id={`${kind}-alerts`} className={`alert-section alert-section--${kind}`} aria-labelledby={`${kind}-title`}>
      <header className="section-heading">
        <div><h2 id={`${kind}-title`}>{title}</h2><p>{deck}</p></div>
        {count !== undefined && <strong aria-label={`${String(count)} ${kind} alerts`}>{count}</strong>}
      </header>
      {loading && <LoadingState label={`Loading ${kind} alerts`} />}
      {error && !alerts && <ErrorState title={`${title} could not be loaded`} message="Other parts of the dashboard may still be available." onRetry={onRetry} />}
      {cached && <ErrorState cached title={`Could not refresh ${kind} alerts`} message="Showing the last available results." onRetry={onRetry} />}
      {alerts?.length === 0 && (
        <div className="empty-state"><strong>No {kind} alerts</strong><p>No {kind} service changes match the selected line.</p></div>
      )}
      <div className="alert-list">
        {visibleAlerts?.map((alert) => <AlertArticle key={alert.id} alert={alert} kind={kind} />)}
      </div>
      {alerts && !preview && <p className="visually-hidden" role="status">Showing {Math.min(visibleCount, alerts.length)} of {count ?? alerts.length} {kind} alerts.</p>}
      {hasPreviewOverflow && alerts && <button className="section-action" type="button" onClick={onViewAll}>View all {count ?? alerts.length} {kind} alerts</button>}
      {hasMore && <button className="section-action" type="button" onClick={showMore}>Show {Math.min(20, (alerts?.length ?? 0) - visibleCount)} more</button>}
    </section>
  )
}
