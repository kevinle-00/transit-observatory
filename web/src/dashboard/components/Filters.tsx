import type { Line } from '../../api/contracts'

export type AlertView = 'all' | 'current' | 'upcoming'

interface FiltersProps {
  lines: Line[] | undefined
  linesFailed: boolean
  lineId: string | null
  view: AlertView
  onLineChange: (value: string) => void
  onViewChange: (value: AlertView) => void
  onRetryLines: () => void
  currentCount: number | undefined
  upcomingCount: number | undefined
}

export function Filters({ lines, linesFailed, lineId, view, onLineChange, onViewChange, onRetryLines, currentCount, upcomingCount }: FiltersProps) {
  const allCount = currentCount !== undefined && upcomingCount !== undefined ? currentCount + upcomingCount : undefined
  const options = [
    { value: 'all' as const, label: 'All', count: allCount },
    { value: 'current' as const, label: 'Current', count: currentCount },
    { value: 'upcoming' as const, label: 'Upcoming', count: upcomingCount },
  ]

  return (
    <section className="alert-toolbar" aria-label="Alert filters">
      <div className="filters__line">
        <label htmlFor="line-filter">Rail line</label>
        <select id="line-filter" value={lines ? lineId ?? '' : ''} onChange={(event) => onLineChange(event.target.value)} disabled={!lines}>
          <option value="">All metropolitan lines</option>
          {lines?.map((line) => <option key={line.id} value={line.id}>{line.long_name || line.short_name}</option>)}
        </select>
        {!lines && <small>{linesFailed ? 'Line list unavailable' : 'Loading line list…'}</small>}
        {linesFailed && lineId && <span className="raw-line-id">Selected ID: <code>{lineId}</code></span>}
        {linesFailed && !lines && <button className="text-button" type="button" onClick={onRetryLines}>Retry line list</button>}
        {lineId && <button className="text-button" type="button" onClick={() => onLineChange('')}>Clear line filter</button>}
      </div>
      <fieldset className="view-filter">
        <legend>Show alerts</legend>
        {options.map((option) => (
          <label key={option.value}>
            <input
              type="radio"
              name="view"
              value={option.value}
              checked={view === option.value}
              aria-label={`${option.label}${option.count !== undefined ? ` ${String(option.count)}` : ''}`}
              onChange={() => onViewChange(option.value)}
            />
            <span className="segment-content"><span>{option.label}</span>{option.count !== undefined && <strong>{option.count}</strong>}</span>
          </label>
        ))}
      </fieldset>
    </section>
  )
}
