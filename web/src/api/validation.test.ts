import { describe, expect, it } from 'vitest'
import { currentEnvelope, freshStatus, lineEnvelope } from '../fixtures/api'
import { envelopeValidators } from './validation'

describe('API response validation', () => {
  it('accepts complete alert, status and line fixtures', () => {
    expect(envelopeValidators.alerts(currentEnvelope)).toBe(true)
    expect(envelopeValidators.status(freshStatus)).toBe(true)
    expect(envelopeValidators.lines(lineEnvelope)).toBe(true)
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
})
