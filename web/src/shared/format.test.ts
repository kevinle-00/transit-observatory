import { describe, expect, it } from 'vitest'
import { cssColor, formatOverallStatus, formatPeriods, humanize, selectTranslation } from './format'

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
})
