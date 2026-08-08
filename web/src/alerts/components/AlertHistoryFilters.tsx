import type { SyntheticEvent } from 'react'
import type { Line, Station } from '../../api/contracts'

export interface HistoryFilterValues {
  lineId: string
  stationId: string
  cause: string
  effect: string
  from: string
  to: string
}

interface Props extends HistoryFilterValues {
  lines: Line[] | undefined
  stations: Station[] | undefined
  rangeError: string | null
  onSubmit: (values: HistoryFilterValues) => void
  onClear: () => void
}

export function AlertHistoryFilters({ lines, stations, rangeError, onSubmit, onClear, ...values }: Props) {
  const submit = (event: SyntheticEvent<HTMLFormElement, SubmitEvent>) => {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const value = (name: string) => {
      const item = data.get(name)
      return typeof item === 'string' ? item : ''
    }
    onSubmit({
      lineId: value('line_id'), stationId: value('station_id'), cause: value('cause').trim(),
      effect: value('effect').trim(), from: value('from'), to: value('to'),
    })
  }

  return (
    <form className="alert-history-filters" aria-label="Past alert filters" onSubmit={submit}>
      <div className="alert-history-filters__grid">
        <label>Rail line<select name="line_id" defaultValue={values.lineId}><option value="">All lines</option>{lines?.map((line) => <option key={line.id} value={line.id}>{line.long_name || line.short_name}</option>)}</select></label>
        <label>Station<select name="station_id" defaultValue={values.stationId}><option value="">All stations</option>{stations?.map((station) => <option key={station.id} value={station.id}>{station.name}</option>)}</select></label>
        <label>Cause<input name="cause" maxLength={64} defaultValue={values.cause} placeholder="e.g. Construction" /></label>
        <label>Effect<input name="effect" maxLength={64} defaultValue={values.effect} placeholder="e.g. No service" /></label>
        <label>Start date<input name="from" type="date" defaultValue={values.from} /></label>
        <label>End date<input name="to" type="date" defaultValue={values.to} /></label>
      </div>
      {rangeError && <p className="alert-history-filters__error" role="alert">{rangeError}</p>}
      <div className="alert-history-filters__actions">
        <button type="submit">Apply filters</button>
        <button type="button" className="alert-feature__secondary" onClick={onClear}>Clear filters</button>
      </div>
    </form>
  )
}
