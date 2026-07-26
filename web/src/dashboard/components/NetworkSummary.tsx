import { Activity, CalendarClock, MapPin, TrainFront } from 'lucide-react'
import type { Line, Status } from '../../api/contracts'
import { cssColor, formatCount } from '../../shared/format'

interface NetworkSummaryProps {
  status: Status
  line: Line | null
  currentCount?: number | undefined
  upcomingCount?: number | undefined
}

export function NetworkSummary({ status, line, currentCount, upcomingCount }: NetworkSummaryProps) {
  const alertCounts = line
    ? {
        current: currentCount ?? line.current_alert_count,
        upcoming: upcomingCount ?? line.upcoming_alert_count,
      }
    : status.service_alerts.counts
  const context = line ? `${line.long_name || line.short_name} line` : 'Whole network'

  return (
    <section className="network-summary" id="overview-metrics" aria-labelledby="network-summary-title" style={{ '--line-accent': cssColor(line?.color, '#6674dc') } as React.CSSProperties}>
      <div className="network-summary__heading">
        <h2 id="network-summary-title">{context}</h2>
        {line && <p>Network context: {formatCount(status.static_gtfs.counts.regular_routes)} regular lines, {formatCount(status.static_gtfs.counts.stations)} stations</p>}
      </div>
      <dl className="summary-metrics">
        <div className="metric-card metric-card--blue"><dt>Current alerts</dt><dd><strong>{formatCount(alertCounts.current)}</strong><span><Activity aria-hidden="true" /></span></dd></div>
        <div className="metric-card metric-card--purple"><dt>Upcoming alerts</dt><dd><strong>{formatCount(alertCounts.upcoming)}</strong><span><CalendarClock aria-hidden="true" /></span></dd></div>
        <div className="metric-card metric-card--cyan"><dt>{line ? 'Network lines' : 'Regular lines'}</dt><dd><strong>{formatCount(status.static_gtfs.counts.regular_routes)}</strong><span><TrainFront aria-hidden="true" /></span></dd></div>
        <div className="metric-card metric-card--green"><dt>{line ? 'Network stations' : 'Stations'}</dt><dd><strong>{formatCount(status.static_gtfs.counts.stations)}</strong><span><MapPin aria-hidden="true" /></span></dd></div>
      </dl>
    </section>
  )
}
