import type { ActivePeriod, AnalyticsInterval, OverallStatus, Translation } from '../api/contracts'

const melbourneDateTime = new Intl.DateTimeFormat('en-AU', {
  timeZone: 'Australia/Melbourne',
  dateStyle: 'medium',
  timeStyle: 'short',
})

const melbourneDate = new Intl.DateTimeFormat('en-AU', {
  timeZone: 'Australia/Melbourne',
  weekday: 'long',
  day: 'numeric',
  month: 'long',
  year: 'numeric',
})

const utcAnalyticsDate = new Intl.DateTimeFormat('en-AU', {
  timeZone: 'UTC',
  day: 'numeric',
  month: 'short',
  year: 'numeric',
})

export function selectTranslation(items: Translation[], language = navigator.language): string | null {
  if (items.length === 0) return null
  const requested = language.toLowerCase()
  const exact = items.find((item) => item.language?.toLowerCase() === requested)
  const base = requested.split('-')[0]
  const family = base ? items.find((item) => item.language?.toLowerCase().split('-')[0] === base) : undefined
  const english = items.find((item) => item.language?.toLowerCase().split('-')[0] === 'en')
  const untagged = items.find((item) => !item.language)
  return (exact ?? family ?? english ?? untagged ?? items[0])?.text ?? null
}

export function cssColor(value: string | undefined, fallback = '#4c5560'): string {
  if (!value) return fallback
  const normalized = value.startsWith('#') ? value : `#${value}`
  return /^#[0-9a-f]{6}$/i.test(normalized) ? normalized : fallback
}

export function formatMelbourneTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? 'Time not available' : melbourneDateTime.format(date)
}

export function formatMelbourneDate(value: Date): string {
  return melbourneDate.format(value)
}

export function formatPeriods(periods: ActivePeriod[]): string[] {
  if (periods.length === 0) return ['No schedule supplied']
  return periods.map((period) => {
    if (!period.starts_at && !period.ends_at) return 'Ongoing, no boundaries supplied'
    if (!period.starts_at && period.ends_at) return `Active until ${formatMelbourneTime(period.ends_at)}`
    if (period.starts_at && !period.ends_at) return `From ${formatMelbourneTime(period.starts_at)}, no scheduled end`
    return `${formatMelbourneTime(period.starts_at ?? '')} to ${formatMelbourneTime(period.ends_at ?? '')}`
  })
}

export function humanize(value: string | undefined): string {
  if (!value) return 'Not specified'
  return value.toLowerCase().split('_').map((word) => word.charAt(0).toUpperCase() + word.slice(1)).join(' ')
}

export function formatCount(value: number): string {
  return new Intl.NumberFormat('en-AU').format(value)
}

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return 'Duration not available'
  const totalSeconds = Math.round(seconds)
  const days = Math.floor(totalSeconds / 86_400)
  const hours = Math.floor(totalSeconds % 86_400 / 3_600)
  const minutes = Math.floor(totalSeconds % 3_600 / 60)
  const remainingSeconds = totalSeconds % 60
  const parts: string[] = []
  if (days > 0) parts.push(`${String(days)} ${days === 1 ? 'day' : 'days'}`)
  if (hours > 0) parts.push(`${String(hours)} ${hours === 1 ? 'hour' : 'hours'}`)
  if (minutes > 0) parts.push(`${String(minutes)} ${minutes === 1 ? 'minute' : 'minutes'}`)
  if (remainingSeconds > 0 || parts.length === 0) parts.push(`${String(remainingSeconds)} ${remainingSeconds === 1 ? 'second' : 'seconds'}`)
  return parts.join(' ')
}

export function formatUtcAnalyticsBucket(value: string, interval: AnalyticsInterval): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Date not available'
  const label = utcAnalyticsDate.format(date)
  return interval === 'week' ? `Week of ${label}` : label
}

export function dateInputToUtcIso(value: string): string | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!match) return null
  const iso = `${value}T00:00:00.000Z`
  const date = new Date(iso)
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value ? iso : null
}

export function utcIsoToDateInput(value: string): string | null {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date.toISOString().slice(0, 10)
}

export function formatOverallStatus(value: OverallStatus): string {
  if (value === 'ok') return 'Up to date'
  if (value === 'degraded') return 'Updates delayed'
  return 'Updates unavailable'
}
