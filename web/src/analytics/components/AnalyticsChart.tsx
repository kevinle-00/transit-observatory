import type { AnalyticsInterval, AnalyticsPoint } from '../../api/contracts'
import { formatCount, formatDuration, formatUtcAnalyticsBucket } from '../../shared/format'

interface AnalyticsChartProps {
  series: AnalyticsPoint[]
  interval: AnalyticsInterval
  lineName: string
}

export function AnalyticsChart({ series, interval, lineName }: AnalyticsChartProps) {
  const width = 720
  const height = 260
  const left = 46
  const top = 20
  const bottom = 36
  const plotWidth = width - left - 18
  const plotHeight = height - top - bottom
  const maximum = Math.max(1, ...series.flatMap((point) => [point.alert_count, point.completed_episode_sample_count]))
  const slot = plotWidth / Math.max(series.length, 1)
  const points = series.map((point, index) => {
    const x = left + slot * index + slot / 2
    const y = top + plotHeight - point.completed_episode_sample_count / maximum * plotHeight
    return `${String(x)},${String(y)}`
  }).join(' ')

  return (
    <div className="analytics-chart-grid">
      <div className="analytics-chart" aria-hidden={series.length === 0}>
        {series.length === 0 ? <p>No observation history is available for this range.</p> : (
          <svg role="img" aria-label={`${lineName} feed-observation episodes and completed samples by ${interval}`} viewBox={`0 0 ${String(width)} ${String(height)}`}>
            <line x1={left} y1={top + plotHeight} x2={width - 18} y2={top + plotHeight} className="analytics-chart__axis" />
            {series.map((point, index) => {
              const barHeight = point.alert_count / maximum * plotHeight
              const x = left + slot * index + slot * 0.18
               return <rect key={point.starts_at} x={x} y={top + plotHeight - barHeight} width={Math.max(slot * 0.48, .35)} height={barHeight} className="analytics-chart__bar"><title>{`${formatUtcAnalyticsBucket(point.starts_at, interval)}: ${formatCount(point.alert_count)} episodes`}</title></rect>
            })}
            <polyline points={points} className="analytics-chart__line" />
            {series.map((point, index) => {
              const x = left + slot * index + slot / 2
              const y = top + plotHeight - point.completed_episode_sample_count / maximum * plotHeight
               return <circle key={point.starts_at} cx={x} cy={y} r={Math.max(Math.min(slot * .18, 4), .4)} className="analytics-chart__point"><title>{`${formatUtcAnalyticsBucket(point.starts_at, interval)}: ${formatCount(point.completed_episode_sample_count)} completed samples`}</title></circle>
            })}
            <text x={left} y={height - 8}>Earlier</text><text x={width - 18} y={height - 8} textAnchor="end">Later (UTC)</text>
          </svg>
        )}
        {series.length > 0 && <p className="analytics-chart__legend"><span className="analytics-chart__bar-key" /> Episodes <span className="analytics-chart__line-key" /> Completed samples</p>}
      </div>
      <div className="analytics-table-wrap">
        <table>
          <caption>{lineName} analytics data</caption>
          <thead><tr><th scope="col">Bucket (UTC)</th><th scope="col">Episodes</th><th scope="col">Completed samples</th><th scope="col">Median lifetime</th></tr></thead>
          <tbody>
            {series.map((point) => <tr key={point.starts_at}>
              <th scope="row">{formatUtcAnalyticsBucket(point.starts_at, interval)}</th>
              <td>{formatCount(point.alert_count)}</td>
              <td>{formatCount(point.completed_episode_sample_count)}</td>
              <td>{point.median_observed_lifetime_seconds === undefined ? 'Not available' : formatDuration(point.median_observed_lifetime_seconds)}</td>
            </tr>)}
          </tbody>
        </table>
      </div>
    </div>
  )
}
