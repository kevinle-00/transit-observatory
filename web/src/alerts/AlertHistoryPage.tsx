import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { historicalAlertsQuery, linesQuery, stationsQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { dateInputToUtcIso, formatCount } from '../shared/format'
import { ErrorState, LoadingState } from '../shared/QueryState'
import { hasControlCharacters, isValidResourceId } from '../shared/resource-id'
import { AlertHistoryCard } from './components/AlertHistoryCard'
import { AlertHistoryFilters, type HistoryFilterValues } from './components/AlertHistoryFilters'
import '../alerts.css'

const pageSize = 25

function positivePage(value: string | null) {
  const page = Number(value)
  return Number.isInteger(page) && page > 0 && page <= 1000 ? page : 1
}

function exclusiveDayAfter(value: string) {
  const start = dateInputToUtcIso(value)
  if (!start) return null
  return new Date(new Date(start).getTime() + 86_400_000).toISOString()
}

function sourceCode(value: string) {
  return value.trim().toUpperCase().replace(/\s+/g, '_')
}

function validOptionalFilter(value: string | null, maxBytes: number) {
  return value === null || new TextEncoder().encode(value).length <= maxBytes && !hasControlCharacters(value)
}

export function AlertHistoryPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  useRoutePage('Alert history | Transit Observatory', heading)
  const [searchParams, setSearchParams] = useSearchParams()
  const [formError, setFormError] = useState<string | null>(null)
  const fromParam = searchParams.get('from')
  const toParam = searchParams.get('to')
  const fromIso = fromParam ? dateInputToUtcIso(fromParam) : null
  const toIso = toParam ? exclusiveDayAfter(toParam) : null
  const lineParam = searchParams.get('line_id')
  const stationParam = searchParams.get('station_id')
  const causeParam = searchParams.get('cause')
  const effectParam = searchParams.get('effect')
  const fromValid = fromParam === null || fromIso !== null
  const toValid = toParam === null || toIso !== null
  const ordered = !fromIso || !toIso || new Date(fromIso).getTime() < new Date(toIso).getTime()
  const bookmarkedFiltersValid = (!lineParam || isValidResourceId(lineParam)) && (!stationParam || isValidResourceId(stationParam)) &&
    validOptionalFilter(causeParam, 64) && validOptionalFilter(effectParam, 64)
  const rangeError = formError ?? (!bookmarkedFiltersValid ? 'One or more filters in this link are invalid. Clear the filters and try again.' :
    !fromValid || !toValid ? 'Dates in this link are invalid. Choose valid dates and apply the filters.' : !ordered ? 'From date must be earlier than through date.' : null)
  const input = {
    ...(lineParam ? { lineId: lineParam } : {}),
    ...(stationParam ? { stationId: stationParam } : {}),
    ...(causeParam ? { cause: causeParam } : {}),
    ...(effectParam ? { effect: effectParam } : {}),
    ...(fromIso ? { from: fromIso } : {}), ...(toIso ? { to: toIso } : {}),
    page: positivePage(searchParams.get('page')), pageSize,
  }
  const history = useQuery({ ...historicalAlertsQuery(input), enabled: rangeError === null })
  const lines = useQuery(linesQuery)
  const stations = useQuery(stationsQuery())

  const applyFilters = (values: HistoryFilterValues) => {
    const from = values.from ? dateInputToUtcIso(values.from) : null
    const to = values.to ? exclusiveDayAfter(values.to) : null
    if ((values.from && !from) || (values.to && !to)) return setFormError('Enter valid from and to dates.')
    if (from && to && new Date(from).getTime() >= new Date(to).getTime()) return setFormError('From date must be earlier than to date.')
    setFormError(null)
    const next = new URLSearchParams()
    const pairs = [['line_id', values.lineId], ['station_id', values.stationId], ['cause', sourceCode(values.cause)], ['effect', sourceCode(values.effect)], ['from', values.from], ['to', values.to]]
    pairs.forEach(([name, value]) => { if (value) next.set(name ?? '', value) })
    setSearchParams(next)
  }

  const changePage = (page: number) => setSearchParams((previous) => {
    const next = new URLSearchParams(previous)
    if (page === 1) next.delete('page'); else next.set('page', String(page))
    return next
  })
  const filterKey = `${searchParams.toString()}-${String(lines.data?.meta.count ?? 0)}-${String(stations.data?.meta.count ?? 0)}`

  return (
    <main id="main" className="workspace feature-page alert-history-page">
      <section className="alert-feature__intro" aria-labelledby="alert-history-title">
        <div><span>Archive</span><h1 id="alert-history-title" ref={heading} tabIndex={-1}>Alert history</h1></div>
        <p>Historical alerts are feed-observation lifecycles: they move here after an alert is no longer present in accepted feed observations. They are not classified as planned or unplanned.</p>
      </section>
      <aside className="alert-feature__limitation">Historical line and station associations use the currently installed GTFS schedule. A source-listed identifier may therefore be unmatched or differ from the network that existed when the alert was observed.</aside>
      <AlertHistoryFilters key={filterKey} lines={lines.data?.data} stations={stations.data?.data} rangeError={rangeError}
        lineId={searchParams.get('line_id') ?? ''} stationId={searchParams.get('station_id') ?? ''}
        cause={searchParams.get('cause') ?? ''} effect={searchParams.get('effect') ?? ''}
        from={fromParam ?? ''} to={toParam ?? ''} onSubmit={applyFilters} onClear={() => { setFormError(null); setSearchParams(new URLSearchParams()) }} />

      <section className="alert-history-results" aria-labelledby="alert-history-results-title">
        <header><div><span>Results</span><h2 id="alert-history-results-title">Historical lifecycles</h2></div>{history.data && <strong>{formatCount(history.data.meta.total)} total</strong>}</header>
        {rangeError === null && history.isPending && <LoadingState label="Loading alert history" />}
        {history.isFetching && !history.isPending && <LoadingState compact label="Updating alert history" />}
        {history.error && !history.data && <ErrorState title="Alert history could not be loaded" message="The historical collection is temporarily unavailable." onRetry={() => void history.refetch()} />}
        {history.error && history.data && <ErrorState cached title="Could not refresh alert history" message="Showing the last available historical results." onRetry={() => void history.refetch()} />}
        {history.data?.data.length === 0 && <div className="empty-state"><strong>No historical alerts found</strong><p>Try broadening the date range or removing a filter.</p></div>}
        {history.data && history.data.data.length > 0 && <div className="alert-history-list">{history.data.data.map((alert) => <AlertHistoryCard key={alert.id} alert={alert} />)}</div>}
        {history.data && history.data.meta.total_pages > 1 && <nav className="alert-history-pagination" aria-label="Alert history pages">
          {history.data.meta.page > 1 ? <button type="button" onClick={() => changePage(history.data.meta.page - 1)}>Previous page</button> : <span />}
          <span>Page {String(history.data.meta.page)} of {String(history.data.meta.total_pages)}</span>
          {history.data.meta.page < history.data.meta.total_pages ? <button type="button" onClick={() => changePage(history.data.meta.page + 1)}>Next page</button> : <span />}
        </nav>}
      </section>
      <p className="alert-feature__back"><Link to="/">Back to current service</Link></p>
    </main>
  )
}
