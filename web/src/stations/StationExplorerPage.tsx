import { useRef, type SyntheticEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { linesQuery, stationsQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { cssColor, formatCount } from '../shared/format'
import { ErrorState, LoadingState } from '../shared/QueryState'
import { hasControlCharacters, isValidResourceId } from '../shared/resource-id'

function stationLink(stationId: string) {
  return { pathname: '/stations/detail', search: new URLSearchParams({ station_id: stationId }).toString() }
}

export function StationExplorerPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  useRoutePage('Stations | Transit Observatory', heading)
  const [params, setParams] = useSearchParams()
  const query = params.get('q') ?? ''
  const lineId = params.get('line_id') ?? ''
  const filtersValid = Array.from(query).length <= 100 && !hasControlCharacters(query) && (!lineId || isValidResourceId(lineId))
  const lines = useQuery(linesQuery)
  const stations = useQuery({ ...stationsQuery({ ...(query ? { q: query } : {}), ...(lineId ? { lineId } : {}) }), enabled: filtersValid })

  const submit = (event: SyntheticEvent<HTMLFormElement, SubmitEvent>) => {
    event.preventDefault()
    const value = new FormData(event.currentTarget).get('q')
    const submitted = typeof value === 'string' ? value : ''
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      if (submitted) next.set('q', submitted)
      else next.delete('q')
      return next
    })
  }

  const selectLine = (value: string) => {
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      if (value) next.set('line_id', value)
      else next.delete('line_id')
      return next
    })
  }

  return (
    <main id="main" className="workspace feature-page station-explorer">
      <header className="explorer-intro">
        <p className="explorer-kicker">Network directory</p>
        <h1 ref={heading} tabIndex={-1}>Stations</h1>
        <p>Search station names and filter by a serving rail line.</p>
      </header>

      <form className="explorer-filters" role="search" onSubmit={submit}>
        <div><label htmlFor="station-search">Station name</label><input key={query} id="station-search" name="q" type="search" maxLength={100} defaultValue={query} /></div>
        <div><label htmlFor="station-line">Serving line</label><select id="station-line" name="line_id" value={lineId} onChange={(event) => selectLine(event.target.value)} disabled={lines.isPending && !lines.data}>
          <option value="">All lines</option>
          {lines.data?.data.map((line) => <option key={line.id} value={line.id}>{line.long_name || line.short_name}</option>)}
        </select></div>
        <button type="submit">Search stations</button>
      </form>
      {!filtersValid && <p className="explorer-filter-error" role="alert">These station filters aren't valid. Enter a station name up to 100 characters and choose a line from the list.</p>}
      {lines.error && !lines.data && <ErrorState title="Line filter unavailable" message="Station search can still be used without a line filter." onRetry={() => void lines.refetch()} />}
      {lines.error && lines.data && <ErrorState cached title="Could not refresh line choices" message="Showing the last available line choices." onRetry={() => void lines.refetch()} />}

      {stations.isPending && <LoadingState label="Loading stations" />}
      {stations.error && !stations.data && <ErrorState title="Stations could not be loaded" message="Try the search again or remove a filter." onRetry={() => void stations.refetch()} />}
      {stations.error && stations.data && <ErrorState cached title="Could not refresh stations" message="Showing the last available search results." onRetry={() => void stations.refetch()} />}
      {stations.data?.data.length === 0 && (
        <section className="explorer-empty" aria-labelledby="empty-stations-title"><h2 id="empty-stations-title">No stations found</h2><p>No stations match your search and selected line.</p></section>
      )}
      {stations.data && stations.data.data.length > 0 && (
        <ul className="explorer-grid" aria-label="Station results">
          {stations.data.data.map((station) => (
            <li key={station.id}><article className="explorer-card">
              <div className="explorer-card__title"><div><h2>{station.name}</h2><code>{station.id}</code></div></div>
              <div className="explorer-serving-lines"><h3>Serving lines</h3><ul>{station.lines.map((line) => <li key={line.id} style={{ '--explorer-accent': cssColor(line.color) } as React.CSSProperties}><span className="explorer-swatch" aria-hidden="true" />{line.long_name || line.short_name}</li>)}</ul></div>
              <dl className="explorer-facts explorer-facts--compact">
                <div><dt>Current alerts</dt><dd>{formatCount(station.current_alert_count)}</dd></div>
                <div><dt>Upcoming alerts</dt><dd>{formatCount(station.upcoming_alert_count)}</dd></div>
              </dl>
              <div className="explorer-actions"><Link to={stationLink(station.id)}>Station details</Link></div>
            </article></li>
          ))}
        </ul>
      )}
    </main>
  )
}
