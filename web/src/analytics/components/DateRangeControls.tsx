import { useSearchParams } from 'react-router-dom'
import type { AnalyticsRange } from '../useAnalyticsRange'

interface DateRangeControlsProps {
  range: AnalyticsRange
}

export function DateRangeControls({ range }: DateRangeControlsProps) {
  const [, setSearchParams] = useSearchParams()
  const update = (name: 'from' | 'to' | 'interval', value: string) => {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set(name, value)
      return next
    })
  }

  return (
    <section className="analytics-controls" aria-labelledby="analytics-range-heading">
      <div className="analytics-controls__heading">
        <h2 id="analytics-range-heading">Date range</h2>
        <p>The start date is included; the end date is not. Dates use UTC.</p>
      </div>
      <div className="analytics-controls__fields">
        <label>From <input type="date" value={range.fromInput} onChange={(event) => update('from', event.target.value)} /></label>
        <label>To <input type="date" value={range.toInput} onChange={(event) => update('to', event.target.value)} /></label>
        <label>Group by
          <select value={range.interval} onChange={(event) => update('interval', event.target.value)}>
            <option value="day">Day</option>
            <option value="week">Week</option>
          </select>
        </label>
      </div>
      {range.error && <p className="analytics-validation" role="alert">{range.error}</p>}
    </section>
  )
}
