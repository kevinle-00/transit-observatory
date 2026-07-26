import type { ActivePeriod, OverallStatus, Translation } from '../api/contracts'

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

export function formatOverallStatus(value: OverallStatus): string {
  if (value === 'ok') return 'Up to date'
  if (value === 'degraded') return 'Updates delayed'
  return 'Updates unavailable'
}
