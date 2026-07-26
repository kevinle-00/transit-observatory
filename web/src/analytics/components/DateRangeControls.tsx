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
        <h2 id="analytics-range-heading">Observation range</h2>
        <p>Includes the start date through the day before the end date, using UTC boundaries.</p>
      </div>
      <div className="analytics-controls__fields">
        <label>From <input type="date" value={range.fromInput} onChange={(event) => update('from', event.target.value)} /></label>
        <label>To <input type="date" value={range.toInput} onChange={(event) => update('to', event.target.value)} /></label>
        <label>Bucket interval
          <select value={range.interval} onChange={(event) => update('interval', event.target.value)}>
            <option value="day">Day</option>
            <option value="week">Week</option>
          </select>
        </label>
      </div>
      {range.error && <p className="analytics-validation" role="alert">{range.error} No analytics request was made.</p>}
    </section>
  )
}
