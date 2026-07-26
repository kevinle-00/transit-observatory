import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { AnalyticsInterval } from '../api/contracts'
import { dateInputToUtcIso } from '../shared/format'

const dayMilliseconds = 86_400_000

export interface AnalyticsRange {
  fromInput: string
  toInput: string
  interval: AnalyticsInterval
  from: string | null
  to: string | null
  error: string | null
}

function defaultInputs(): Pick<AnalyticsRange, 'fromInput' | 'toInput'> {
  const now = new Date()
  const to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1))
  const from = new Date(to.getTime() - 30 * dayMilliseconds)
  return { fromInput: from.toISOString().slice(0, 10), toInput: to.toISOString().slice(0, 10) }
}

export function useAnalyticsRange(): AnalyticsRange {
  const [searchParams, setSearchParams] = useSearchParams()
  const [defaults] = useState(defaultInputs)
  const fromInput = searchParams.get('from') ?? defaults.fromInput
  const toInput = searchParams.get('to') ?? defaults.toInput
  const rawInterval = searchParams.get('interval')
  const interval: AnalyticsInterval = rawInterval === 'week' ? 'week' : 'day'
  const from = dateInputToUtcIso(fromInput)
  const to = dateInputToUtcIso(toInput)
  let error: string | null = null

  if (!from || !to) error = 'Enter complete dates in YYYY-MM-DD format.'
  else if (new Date(from).getTime() >= new Date(to).getTime()) error = 'The from date must be earlier than the to date.'
  else if (new Date(to).getTime() - new Date(from).getTime() > 366 * dayMilliseconds) error = 'Choose a date range of no more than 366 days.'

  useEffect(() => {
    if (searchParams.has('from') && searchParams.has('to') && (rawInterval === 'day' || rawInterval === 'week')) return
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      if (!next.has('from')) next.set('from', defaults.fromInput)
      if (!next.has('to')) next.set('to', defaults.toInput)
      if (rawInterval !== 'day' && rawInterval !== 'week') next.set('interval', 'day')
      return next
    }, { replace: true })
  }, [defaults.fromInput, defaults.toInput, rawInterval, searchParams, setSearchParams])

  return { fromInput, toInput, interval, from, to, error }
}
