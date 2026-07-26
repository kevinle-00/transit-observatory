import { describe, expect, it } from 'vitest'
import {
  alertDetailEnvelope, alertRevisionsEnvelope, currentEnvelope, freshStatus, historicalEnvelope,
  lineAnalyticsDetailEnvelope, lineAnalyticsEnvelope, lineDetailEnvelope, lineEnvelope,
  stationDetailEnvelope, stationEnvelope,
} from '../fixtures/api'
import { envelopeValidators } from './validation'

describe('API response validation', () => {
  it('accepts complete alert, status and line fixtures', () => {
    expect(envelopeValidators.alerts(currentEnvelope)).toBe(true)
    expect(envelopeValidators.status(freshStatus)).toBe(true)
    expect(envelopeValidators.lines(lineEnvelope)).toBe(true)
  })

  it('accepts every detail, collection and analytics contract', () => {
    expect(envelopeValidators.historicalAlerts(historicalEnvelope)).toBe(true)
    expect(envelopeValidators.alertDetail(alertDetailEnvelope.data.id)(alertDetailEnvelope)).toBe(true)
    expect(envelopeValidators.alertRevisions(alertDetailEnvelope.data.id)(alertRevisionsEnvelope)).toBe(true)
    expect(envelopeValidators.lineDetail(lineDetailEnvelope.data.line.id)(lineDetailEnvelope)).toBe(true)
    expect(envelopeValidators.stations(stationEnvelope)).toBe(true)
    expect(envelopeValidators.stationDetail(stationDetailEnvelope.data.station.id)(stationDetailEnvelope)).toBe(true)
    expect(envelopeValidators.lineAnalytics(lineAnalyticsEnvelope)).toBe(true)
    expect(envelopeValidators.lineAnalyticsDetail(lineAnalyticsDetailEnvelope.data.line.id)(lineAnalyticsDetailEnvelope)).toBe(true)
  })

  it('asserts the requested alert status and resource ids', () => {
    expect(envelopeValidators.alertsForStatus('current')(currentEnvelope)).toBe(true)
    expect(envelopeValidators.alertsForStatus('upcoming')(currentEnvelope)).toBe(false)
    expect(envelopeValidators.alertDetail(999)(alertDetailEnvelope)).toBe(false)
    expect(envelopeValidators.alertRevisions(999)(alertRevisionsEnvelope)).toBe(false)
    expect(envelopeValidators.lineDetail('another-line')(lineDetailEnvelope)).toBe(false)
    expect(envelopeValidators.stationDetail('another-station')(stationDetailEnvelope)).toBe(false)
  })

  it('rejects nested alert timestamps that cannot be parsed', () => {
    const response = {
      ...currentEnvelope,
      data: [{ ...currentEnvelope.data[0], active_periods: [{ position: 0, starts_at: 'not-a-date' }] }],
    }
    expect(envelopeValidators.alerts(response)).toBe(false)
  })

  it('rejects unknown status classifications', () => {
    const response = { ...freshStatus, data: { ...freshStatus.data, overall_status: 'mostly-fine' } }
    expect(envelopeValidators.status(response)).toBe(false)
  })

  it('rejects negative and fractional line counts', () => {
    const negative = { ...lineEnvelope, data: [{ ...lineEnvelope.data[0], station_count: -1 }] }
    const fractional = { ...lineEnvelope, data: [{ ...lineEnvelope.data[0], current_alert_count: 1.5 }] }
    expect(envelopeValidators.lines(negative)).toBe(false)
    expect(envelopeValidators.lines(fractional)).toBe(false)
  })

  it('validates historical page counts separately from the overall total', () => {
    expect(envelopeValidators.historicalAlerts({
      ...historicalEnvelope,
      meta: { ...historicalEnvelope.meta, total: 51, total_pages: 3 },
    })).toBe(true)
    expect(envelopeValidators.historicalAlerts({
      ...historicalEnvelope,
      meta: { ...historicalEnvelope.meta, count: 18 },
    })).toBe(false)
    expect(envelopeValidators.historicalAlerts({
      ...historicalEnvelope,
      meta: { ...historicalEnvelope.meta, total: 51, total_pages: 2 },
    })).toBe(false)
  })

  it('rejects null for omitted lifecycle and station fields', () => {
    expect(envelopeValidators.alertDetail()({
      ...alertDetailEnvelope,
      data: { ...alertDetailEnvelope.data, closed_at: null },
    })).toBe(false)
    expect(envelopeValidators.stations({
      ...stationEnvelope,
      data: [{ ...stationEnvelope.data[0], latitude: null }],
    })).toBe(false)
  })

  it('rejects malformed nested details and inconsistent guaranteed counts', () => {
    expect(envelopeValidators.alertDetail()({
      ...alertDetailEnvelope,
      data: { ...alertDetailEnvelope.data, latest_revision: { ...alertDetailEnvelope.data.latest_revision, is_deleted: 'yes' } },
    })).toBe(false)
    expect(envelopeValidators.alertRevisions()({ ...alertRevisionsEnvelope, meta: { count: 9 } })).toBe(false)
    expect(envelopeValidators.lineDetail()({
      ...lineDetailEnvelope,
      data: { ...lineDetailEnvelope.data, line: { ...lineDetailEnvelope.data.line, station_count: 9 } },
    })).toBe(false)
    expect(envelopeValidators.stationDetail()({
      ...stationDetailEnvelope,
      data: { ...stationDetailEnvelope.data, alerts: [{ ...stationDetailEnvelope.data.alerts[0], id: 0 }] },
    })).toBe(false)
  })

  it('requires exact analytics metadata, limitations and finite nested metrics', () => {
    expect(envelopeValidators.lineAnalytics({
      ...lineAnalyticsEnvelope,
      meta: { ...lineAnalyticsEnvelope.meta, count: 2 },
    })).toBe(false)
    expect(envelopeValidators.lineAnalytics({
      ...lineAnalyticsEnvelope,
      meta: { ...lineAnalyticsEnvelope.meta, timezone: 'Australia/Melbourne' },
    })).toBe(false)
    expect(envelopeValidators.lineAnalyticsDetail()({
      ...lineAnalyticsDetailEnvelope,
      meta: { ...lineAnalyticsDetailEnvelope.meta, count: 0 },
    })).toBe(false)
    expect(envelopeValidators.lineAnalyticsDetail()({
      ...lineAnalyticsDetailEnvelope,
      data: { ...lineAnalyticsDetailEnvelope.data, metric_limitations: ['Something else'] },
    })).toBe(false)
    expect(envelopeValidators.lineAnalytics({
      ...lineAnalyticsEnvelope,
      data: [{
        ...lineAnalyticsEnvelope.data[0],
        series: [{ ...lineAnalyticsEnvelope.data[0]?.series[0], median_observed_lifetime_seconds: Number.NaN }],
      }],
    })).toBe(false)
    expect(envelopeValidators.lineAnalytics({
      ...lineAnalyticsEnvelope,
      data: [{
        ...lineAnalyticsEnvelope.data[0],
        series: [{ ...lineAnalyticsEnvelope.data[0]?.series[0], starts_at: '2026-07-20T10:00:00+10:00' }],
      }],
    })).toBe(true)
  })

  it('rejects contradictory periods, counts, coordinates and analytics series', () => {
    expect(envelopeValidators.alerts({
      ...currentEnvelope,
      data: [{ ...currentEnvelope.data[0], active_periods: [{ position: 0, starts_at: '2026-07-27T02:00:00Z', ends_at: '2026-07-27T01:00:00Z' }] }],
    })).toBe(false)
    expect(envelopeValidators.lines({
      ...lineEnvelope,
      data: [{ ...lineEnvelope.data[0], present_alert_count: 0, current_alert_count: 1 }],
      meta: { count: 1 },
    })).toBe(false)
    expect(envelopeValidators.stations({
      ...stationEnvelope,
      data: [{ ...stationEnvelope.data[0], latitude: 91 }],
    })).toBe(false)
    expect(envelopeValidators.lineAnalytics({
      ...lineAnalyticsEnvelope,
      data: [{ ...lineAnalyticsEnvelope.data[0], series: [{ starts_at: '2026-07-20T00:00:00Z', alert_count: 1, completed_episode_sample_count: 2 }] }],
    })).toBe(false)
    expect(envelopeValidators.lineAnalytics({
      ...lineAnalyticsEnvelope,
      data: [{ ...lineAnalyticsEnvelope.data[0], series: [...(lineAnalyticsEnvelope.data[0]?.series ?? [])].reverse() }],
    })).toBe(false)
  })
})
