import { useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { lineAnalyticsQuery } from '../api/queries'
import { useRoutePage } from '../app/use-route-page'
import { cssColor, formatCount } from '../shared/format'
import { AnalyticsError, AnalyticsLoading } from './components/AnalyticsStates'
import { DateRangeControls } from './components/DateRangeControls'
import { useAnalyticsRange } from './useAnalyticsRange'

function lineLink(path: string, id: string, range?: { from: string; to: string; interval: string }): string {
  return `${path}?${new URLSearchParams({ line_id: id, ...(range ?? {}) }).toString()}`
}

export function AnalyticsOverviewPage() {
  const heading = useRef<HTMLHeadingElement>(null)
  useRoutePage('Line analytics | Transit Observatory', heading)
  const range = useAnalyticsRange()
  const valid = !range.error && range.from !== null && range.to !== null
  const analytics = useQuery({
    ...lineAnalyticsQuery({
      ...(range.from ? { from: range.from } : {}),
      ...(range.to ? { to: range.to } : {}),
      interval: range.interval,
    }),
    enabled: valid,
  })
  const lines = analytics.data?.data.filter((item) => !item.line.is_replacement_bus) ?? []

  return (
    <main id="main" className="workspace feature-page analytics-page analytics-overview">
      <section className="page-intro" aria-labelledby="analytics-title">
        <div><span>Historical observations</span><h1 id="analytics-title" ref={heading} tabIndex={-1}>Line analytics</h1></div>
        <p>Compare continuous feed-observation episodes recorded for regular rail lines.</p>
      </section>
      <aside className="analytics-definition" aria-label="Metric context">
        These counts are feed-observation episodes, not incidents, passenger impact, reliability, or punctuality measures.
      </aside>
      <DateRangeControls range={range} />
      {analytics.isPending && valid && <AnalyticsLoading label="Loading line analytics" />}
      {analytics.error && !analytics.data && <AnalyticsError onRetry={() => void analytics.refetch()} />}
      {analytics.error && analytics.data && <AnalyticsError cached onRetry={() => void analytics.refetch()} />}
      {analytics.data && lines.length === 0 && <section className="analytics-empty"><h2>No line observations</h2><p>No regular lines have observation history in this range.</p></section>}
      {lines.length > 0 && <section className="analytics-line-list" aria-label="Regular line analytics">
        {lines.map((item) => {
          const episodes = item.series.reduce((total, point) => total + point.alert_count, 0)
          const samples = item.series.reduce((total, point) => total + point.completed_episode_sample_count, 0)
          return <article className="analytics-line-card" key={item.line.id} style={{ '--line-color': cssColor(item.line.color) } as React.CSSProperties}>
            <div className="analytics-line-card__title"><i aria-hidden="true" /><div><h2>{item.line.short_name}</h2><p>{item.line.long_name}</p></div></div>
            <dl><div><dt>Alerts observed</dt><dd>{formatCount(episodes)}</dd></div><div><dt>Completed lifetimes</dt><dd>{formatCount(samples)}</dd></div><div><dt>{range.interval === 'week' ? 'Weeks' : 'Days'} shown</dt><dd>{formatCount(item.series.length)}</dd></div></dl>
            <p className="analytics-sample-note">Lifetime statistics can only use the {formatCount(samples)} alert {samples === 1 ? 'lifecycle' : 'lifecycles'} observed through completion.</p>
            <div className="analytics-card-actions"><Link to={lineLink('/analytics/line', item.line.id, { from: range.fromInput, to: range.toInput, interval: range.interval })}>View analytics</Link><Link to={lineLink('/lines/detail', item.line.id)}>View line</Link></div>
          </article>
        })}
      </section>}
    </main>
  )
}
