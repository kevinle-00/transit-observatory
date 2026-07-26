import { describe, expect, it } from 'vitest'
import {
  cssColor, dateInputToUtcIso, formatDuration, formatOverallStatus, formatPeriods,
  formatUtcAnalyticsBucket, humanize, selectTranslation, utcIsoToDateInput,
} from './format'

describe('display formatters', () => {
  it('selects requested language then English, untagged and first', () => {
    const translated = [{ text: 'Premier', language: 'fr' }, { text: 'First', language: 'en' }, { text: 'Default' }]
    expect(selectTranslation(translated, 'fr-CA')).toBe('Premier')
    expect(selectTranslation(translated, 'de')).toBe('First')
    expect(selectTranslation([{ text: 'Default' }, { text: 'Premier', language: 'fr' }], 'de')).toBe('Default')
    expect(selectTranslation([{ text: 'Premier', language: 'fr' }], 'de')).toBe('Premier')
  })

  it('accepts only six-digit GTFS colors', () => {
    expect(cssColor('006F45')).toBe('#006F45')
    expect(cssColor('#abcdef')).toBe('#abcdef')
    expect(cssColor('red')).toBe('#4c5560')
    expect(cssColor('123456; background:red')).toBe('#4c5560')
  })

  it('describes absent and open active periods', () => {
    expect(formatPeriods([])).toEqual(['No schedule supplied'])
    expect(formatPeriods([{ position: 0 }])).toEqual(['Ongoing, no boundaries supplied'])
    expect(formatPeriods([{ position: 0, starts_at: '2026-07-26T10:00:00Z' }])[0]).toContain('no scheduled end')
  })

  it('humanizes backend classifications without reclassifying them', () => {
    expect(humanize('SIGNIFICANT_DELAYS')).toBe('Significant Delays')
    expect(humanize(undefined)).toBe('Not specified')
  })

  it('translates technical source health into public status language', () => {
    expect(formatOverallStatus('ok')).toBe('Up to date')
    expect(formatOverallStatus('degraded')).toBe('Updates delayed')
    expect(formatOverallStatus('unavailable')).toBe('Updates unavailable')
  })

  it('formats finite durations without discarding useful units', () => {
    expect(formatDuration(0)).toBe('0 seconds')
    expect(formatDuration(61)).toBe('1 minute 1 second')
    expect(formatDuration(90_061)).toBe('1 day 1 hour 1 minute 1 second')
    expect(formatDuration(Number.NaN)).toBe('Duration not available')
    expect(formatDuration(-1)).toBe('Duration not available')
  })

  it('labels analytics buckets in UTC', () => {
    expect(formatUtcAnalyticsBucket('2026-07-20T00:00:00Z', 'day')).toBe('20 July 2026')
    expect(formatUtcAnalyticsBucket('2026-07-20T00:00:00Z', 'week')).toBe('Week of 20 July 2026')
    expect(formatUtcAnalyticsBucket('not-a-date', 'day')).toBe('Date not available')
  })

  it('converts date inputs at the UTC day boundary and rejects invalid dates', () => {
    expect(dateInputToUtcIso('2026-07-20')).toBe('2026-07-20T00:00:00.000Z')
    expect(dateInputToUtcIso('2026-02-30')).toBeNull()
    expect(dateInputToUtcIso('20/07/2026')).toBeNull()
    expect(utcIsoToDateInput('2026-07-20T23:59:59-07:00')).toBe('2026-07-21')
    expect(utcIsoToDateInput('invalid')).toBeNull()
  })
})
