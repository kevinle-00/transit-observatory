import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { lineAnalyticsDetailQuery } from '../api/queries'
import type { AnalyticsBreakdown } from '../api/contracts'
import { useRoutePage } from '../app/use-route-page'
import { cssColor, formatCount, humanize } from '../shared/format'
import { isNotFoundError } from '../shared/api-errors'
import { ResourceNotFound } from '../shared/ResourceNotFound'
import { isValidResourceId } from '../shared/resource-id'
import { AnalyticsChart } from './components/AnalyticsChart'
import { AnalyticsError, AnalyticsLoading } from './components/AnalyticsStates'
import { DateRangeControls } from './components/DateRangeControls'
import { useAnalyticsRange } from './useAnalyticsRange'

function Breakdown({ title, items }: { title: string; items: AnalyticsBreakdown[] }) {
  return <section className="analytics-breakdown"><h2>{title}</h2>{items.length === 0 ? <p>No data for this range.</p> : <ul>{items.map((item) => <li key={item.value}><span>{humanize(item.value)}</span><strong>{formatCount(item.count)}</strong></li>)}</ul>}</section>
}

export function LineAnalyticsPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  const [searchParams] = useSearchParams()
  const lineId = searchParams.get('line_id') ?? ''
  const range = useAnalyticsRange()
  const lineIdValid = isValidResourceId(lineId)
  const valid = lineIdValid && !range.error && range.from !== null && range.to !== null
  const analytics = useQuery({
    ...lineAnalyticsDetailQuery(lineId, {
      ...(range.from ? { from: range.from } : {}),
      ...(range.to ? { to: range.to } : {}),
      interval: range.interval,
    }),
    enabled: valid,
  })
  const analyticsNotFound = isNotFoundError(analytics.error)
  const lineName = analytics.data?.data.line.short_name
  useRoutePage(lineName ? `${lineName} analytics | Transit Observatory` : 'Line analytics | Transit Observatory', heading)
  const data = analytics.data?.data
  const meta = analytics.data?.meta
  const episodes = data?.series.reduce((total, point) => total + point.alert_count, 0) ?? 0
  const samples = data?.series.reduce((total, point) => total + point.completed_episode_sample_count, 0) ?? 0
  const medianBuckets = data?.series.filter((point) => point.median_observed_lifetime_seconds !== undefined).length ?? 0

  return (
    <main id="main" className="workspace feature-page analytics-page analytics-detail">
      <section className="page-intro" aria-labelledby="line-analytics-title">
        <div><span>Alert history</span><h1 id="line-analytics-title" ref={heading} tabIndex={-1}>{lineName ? `${lineName} line analytics` : 'Line analytics'}</h1></div>
        <p>{data?.line.long_name ?? "Explore a line's alert history."}</p>
      </section>
      {!lineIdValid && <p className="analytics-validation" role="alert">Choose a line from the analytics overview.</p>}
      {lineIdValid && <DateRangeControls range={range} />}
      {analytics.isPending && valid && <AnalyticsLoading label="Loading line analytics" />}
      {analyticsNotFound && <ResourceNotFound resource="Line analytics" backTo="/analytics" backLabel="Return to analytics overview" />}
      {analytics.error && !analytics.data && !analyticsNotFound && <AnalyticsError onRetry={() => void analytics.refetch()} />}
      {analytics.error && analytics.data && !analyticsNotFound && <AnalyticsError cached onRetry={() => void analytics.refetch()} />}
      {data && meta && !analyticsNotFound && <>
        <section className="analytics-line-heading" style={{ '--line-color': cssColor(data.line.color) } as React.CSSProperties}>
          <i aria-hidden="true" /><p>Line ID: <code>{data.line.id}</code></p><Link to={`/lines/detail?${new URLSearchParams({ line_id: data.line.id }).toString()}`}>View line details</Link>
        </section>
        <aside className="analytics-definition" aria-label="About these numbers">These numbers show how many alerts were recorded over time. They do not measure incidents, affected passengers, reliability, or punctuality.</aside>
        <dl className="analytics-summary">
          <div><dt>Alerts recorded</dt><dd>{formatCount(episodes)}</dd></div>
          <div><dt>Alerts that ended</dt><dd>{formatCount(samples)}</dd></div>
          <div><dt>{meta.interval === 'week' ? 'Weeks' : 'Days'} shown</dt><dd>{formatCount(data.series.length)}</dd></div>
          <div><dt>{meta.interval === 'week' ? 'Weeks' : 'Days'} with duration data</dt><dd>{formatCount(medianBuckets)}</dd></div>
        </dl>
        {data.series.length === 0 && <section className="analytics-empty"><h2>No alert history</h2><p>No alerts were found for this line and date range.</p></section>}
        <section className="analytics-visualization" aria-labelledby="history-heading"><h2 id="history-heading">Alerts over time</h2><p>Bars show recorded alerts. The line shows alerts with a known end time.</p><AnalyticsChart series={data.series} interval={meta.interval} lineName={data.line.short_name} /></section>
        <div className="analytics-breakdowns"><Breakdown title="Reported causes" items={data.causes} /><Breakdown title="Reported effects" items={data.effects} /></div>
        <section className="analytics-metadata" aria-labelledby="metadata-heading"><h2 id="metadata-heading">About these numbers</h2><ul>{data.metric_limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul><details><summary>Technical details</summary><dl><div><dt>Date range (UTC)</dt><dd><code>{meta.from}</code> to <code>{meta.to}</code> (end date not included)</dd></div><div><dt>Grouped by</dt><dd>{humanize(meta.interval)} in {meta.timezone}</dd></div><div><dt>How alerts are counted</dt><dd>Each alert is counted again if it disappears and later returns.</dd></div></dl></details></section>
      </>}
    </main>
  )
}
