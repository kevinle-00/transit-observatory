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
  return <section className="analytics-breakdown"><h2>{title}</h2>{items.length === 0 ? <p>No values were observed in this range.</p> : <ul>{items.map((item) => <li key={item.value}><span>{humanize(item.value)}</span><strong>{formatCount(item.count)}</strong></li>)}</ul>}</section>
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
        <div><span>Historical observations</span><h1 id="line-analytics-title" ref={heading} tabIndex={-1}>{lineName ? `${lineName} line analytics` : 'Line analytics'}</h1></div>
        <p>{data?.line.long_name ?? 'Inspect a line’s feed-observation history.'}</p>
      </section>
      {!lineIdValid && <p className="analytics-validation" role="alert">Choose a valid line from the analytics overview. No analytics request was made.</p>}
      {lineIdValid && <DateRangeControls range={range} />}
      {analytics.isPending && valid && <AnalyticsLoading label="Loading line analytics" />}
      {analyticsNotFound && <ResourceNotFound resource="Line analytics" backTo="/analytics" backLabel="Return to analytics overview" />}
      {analytics.error && !analytics.data && !analyticsNotFound && <AnalyticsError onRetry={() => void analytics.refetch()} />}
      {analytics.error && analytics.data && !analyticsNotFound && <AnalyticsError cached onRetry={() => void analytics.refetch()} />}
      {data && meta && !analyticsNotFound && <>
        <section className="analytics-line-heading" style={{ '--line-color': cssColor(data.line.color) } as React.CSSProperties}>
          <i aria-hidden="true" /><p>Raw line identifier: <code>{data.line.id}</code></p><Link to={`/lines/detail?${new URLSearchParams({ line_id: data.line.id }).toString()}`}>View line details</Link>
        </section>
        <aside className="analytics-definition" aria-label="Metric context">These are feed-observation episodes. They do not measure incidents, passenger impact, reliability, or punctuality.</aside>
        <dl className="analytics-summary">
          <div><dt>Alerts observed</dt><dd>{formatCount(episodes)}</dd></div>
          <div><dt>Completed lifetimes</dt><dd>{formatCount(samples)}</dd></div>
          <div><dt>{meta.interval === 'week' ? 'Weeks' : 'Days'} shown</dt><dd>{formatCount(data.series.length)}</dd></div>
          <div><dt>{meta.interval === 'week' ? 'Weeks' : 'Days'} with a median</dt><dd>{formatCount(medianBuckets)}</dd></div>
        </dl>
        {data.series.length === 0 && <section className="analytics-empty"><h2>No observation history</h2><p>The backend returned zero buckets for this line and range. No lifetime or trend can be reported.</p></section>}
        <section className="analytics-visualization" aria-labelledby="history-heading"><h2 id="history-heading">Observation history</h2><p>Bars show episodes; the line shows completed samples used as lifetime context.</p><AnalyticsChart series={data.series} interval={meta.interval} lineName={data.line.short_name} /></section>
        <div className="analytics-breakdowns"><Breakdown title="Reported causes" items={data.causes} /><Breakdown title="Reported effects" items={data.effects} /></div>
        <section className="analytics-metadata" aria-labelledby="metadata-heading"><h2 id="metadata-heading">Metric definitions and limitations</h2><ul>{data.metric_limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul><details><summary>Technical boundaries</summary><dl><div><dt>UTC range boundaries</dt><dd><code>{meta.from}</code> to <code>{meta.to}</code> (end excluded)</dd></div><div><dt>Bucket interval</dt><dd>{humanize(meta.interval)} buckets in {meta.timezone}</dd></div><div><dt>Metric basis</dt><dd><code>{meta.metric_basis}</code></dd></div></dl></details></section>
      </>}
    </main>
  )
}
