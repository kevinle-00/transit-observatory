import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { alertsQuery, linesQuery, statusQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { ErrorState, LoadingState } from '../shared/QueryState'
import { AlertSection } from './components/AlertSection'
import { Filters, type AlertView } from './components/Filters'
import { FreshnessNotice } from './components/FreshnessNotice'
import { NetworkSummary } from './components/NetworkSummary'

function readView(value: string | null): AlertView {
  return value === 'current' || value === 'upcoming' ? value : 'all'
}

export function Dashboard() {
  const heading = useRef<HTMLHeadingElement>(null)
  useRoutePage('Melbourne network | Transit Observatory', heading)
  const [searchParams, setSearchParams] = useSearchParams()
  const lineId = searchParams.get('line_id') || null
  const rawView = searchParams.get('view')
  const view = readView(rawView)
  const status = useQuery(statusQuery)
  const lines = useQuery(linesQuery)
  const selectedLine = lines.data?.data.find((line) => line.id === lineId) ?? null
  const lineFilterReady = lineId === null || selectedLine !== null || lines.isError
  const effectiveLineId = selectedLine?.id ?? (lines.isError ? lineId : null)
  const current = useQuery({ ...alertsQuery('current', effectiveLineId), enabled: lineFilterReady })
  const upcoming = useQuery({ ...alertsQuery('upcoming', effectiveLineId), enabled: lineFilterReady })
  const allUnavailable = [status, lines, current, upcoming].every((query) => query.isError && !query.data)

  useEffect(() => {
    if (!lineId || !lines.data || selectedLine) return
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.delete('line_id')
      return next
    }, { replace: true })
  }, [lineId, lines.data, selectedLine, setSearchParams])

  useEffect(() => {
    if (!rawView || rawView === 'current' || rawView === 'upcoming') return
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.delete('view')
      return next
    }, { replace: true })
  }, [rawView, setSearchParams])

  const updateParam = (name: 'line_id' | 'view', value: string) => {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      if (!value || (name === 'view' && value === 'all')) next.delete(name)
      else next.set(name, value)
      return next
    })
  }

  if (allUnavailable) {
    return (
      <main id="main" className="workspace unavailable-page">
        <span className="page-label">Connection problem</span>
        <h1 ref={heading} tabIndex={-1}>We couldn't load the dashboard.</h1>
        <p>We couldn't reach the latest service updates. Your filters are still saved.</p>
        <button type="button" onClick={() => void Promise.all([status.refetch(), lines.refetch(), current.refetch(), upcoming.refetch()])}>Try again</button>
      </main>
    )
  }

  return (
      <main id="main" className="workspace">
        <section className="page-intro" aria-labelledby="page-title">
          <div><span>Network dashboard</span><h1 id="page-title" ref={heading} tabIndex={-1}>Melbourne rail service</h1></div>
          <p>Current and upcoming service changes across Melbourne's rail network.</p>
        </section>

        <section className="freshness-strip" aria-label="Update status">
          {status.isPending && <LoadingState compact label="Checking for updates" />}
          {status.error && !status.data && <ErrorState title="Update status is unavailable" message="Alerts may still load, but we can't confirm how current they are." onRetry={() => void status.refetch()} />}
          {status.error && status.data && <ErrorState cached title="Could not check for updates" message="Showing the last available status." onRetry={() => void status.refetch()} />}
          {status.data && <FreshnessNotice status={status.data.data} />}
        </section>

        {status.data && (!lineId || selectedLine) && (
          <NetworkSummary
            status={status.data.data}
            line={selectedLine}
            currentCount={current.data?.meta.count}
            upcomingCount={upcoming.data?.meta.count}
          />
        )}

        <Filters
          lines={lines.data?.data}
          linesFailed={lines.isError}
          lineId={selectedLine?.id ?? (lines.isError ? lineId : null)}
          view={view}
          onLineChange={(value) => updateParam('line_id', value)}
          onViewChange={(value) => updateParam('view', value)}
          onRetryLines={() => void lines.refetch()}
          currentCount={current.data?.meta.count}
          upcomingCount={upcoming.data?.meta.count}
        />

        <div className={`alert-results alert-results--${view}`}>
          {view !== 'upcoming' && <AlertSection
            key={`current-${view}-${effectiveLineId ?? 'network'}`}
            kind="current"
            alerts={current.data?.data}
            count={current.data?.meta.count}
            loading={current.isPending}
            error={Boolean(current.error && !current.data)}
            cached={Boolean(current.error && current.data)}
            preview={view === 'all'}
            onRetry={() => void current.refetch()}
            onViewAll={() => updateParam('view', 'current')}
          />}
          {view !== 'current' && <AlertSection
            key={`upcoming-${view}-${effectiveLineId ?? 'network'}`}
            kind="upcoming"
            alerts={upcoming.data?.data}
            count={upcoming.data?.meta.count}
            loading={upcoming.isPending}
            error={Boolean(upcoming.error && !upcoming.data)}
            cached={Boolean(upcoming.error && upcoming.data)}
            preview={view === 'all'}
            onRetry={() => void upcoming.refetch()}
            onViewAll={() => updateParam('view', 'upcoming')}
          />}
        </div>
      </main>
  )
}
