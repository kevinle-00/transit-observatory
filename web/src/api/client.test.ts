import { beforeEach, describe, expect, it, vi } from 'vitest'
import { currentEnvelope, freshStatus, lineEnvelope } from '../fixtures/api'
import { ApiError, getJson, shouldRetryRequest } from './client'
import { envelopeValidators } from './validation'

describe('API client', () => {
  beforeEach(() => vi.unstubAllGlobals())

  it('returns validated JSON and uses the configured API path', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response(JSON.stringify({ data: [], meta: { count: 0 } }), { status: 200 })))
    vi.stubGlobal('fetch', fetchMock)
    await expect(getJson('/api/v1/lines', envelopeValidators.lines)).resolves.toEqual({ data: [], meta: { count: 0 } })
    expect(fetchMock).toHaveBeenCalledWith('http://localhost:8080/api/v1/lines', expect.objectContaining({ headers: { Accept: 'application/json' } }))
  })

  it('exposes backend status, code and public message', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ error: { code: 'request_timeout', message: 'request timed out' } }), { status: 504 }))))
    const promise = getJson('/api/v1/status', envelopeValidators.status)
    await expect(promise).rejects.toMatchObject({ status: 504, code: 'request_timeout', publicMessage: 'request timed out' })
  })

  it('rejects successful responses with invalid envelopes', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ results: [] }), { status: 200 }))))
    await expect(getJson('/api/v1/lines', envelopeValidators.lines)).rejects.toMatchObject({ code: 'invalid_response' })
  })

  it('normalizes transport failures as public API errors', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('private network detail'))))
    await expect(getJson('/api/v1/status', envelopeValidators.status)).rejects.toMatchObject({
      status: 0, code: 'network_error', publicMessage: 'The observatory could not be reached.',
    })
  })

  it.each([
    ['alert', { ...currentEnvelope, data: [{ ...currentEnvelope.data[0], routes: [{ source_route_id: 'bad', is_matched: 'yes' }] }] }, envelopeValidators.alerts],
    ['status', { ...freshStatus, data: { ...freshStatus.data, service_alerts: { ...freshStatus.data.service_alerts, counts: { current: -1 } } } }, envelopeValidators.status],
    ['line', { ...lineEnvelope, data: [{ ...lineEnvelope.data[0], station_count: Number.NaN }] }, envelopeValidators.lines],
  ])('rejects a successful response with malformed nested %s data', async (_name, body, validator) => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))))
    const wrappedValidator = (value: unknown): value is unknown => validator(value)
    await expect(getJson('/api/v1/test', wrappedValidator)).rejects.toMatchObject({ code: 'invalid_response' })
  })

  it.each([
    ['timestamp without a zone', { ...currentEnvelope, data: [{ ...currentEnvelope.data[0], first_seen_at: '2026-07-26T09:00:00' }] }, envelopeValidators.alerts],
    ['impossible calendar date', { ...currentEnvelope, data: [{ ...currentEnvelope.data[0], first_seen_at: '2026-02-30T09:00:00Z' }] }, envelopeValidators.alerts],
    ['inconsistent count', { ...lineEnvelope, meta: { count: lineEnvelope.data.length + 1 } }, envelopeValidators.lines],
  ])('rejects a successful response with %s', async (_name, body, validator) => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))))
    const wrappedValidator = (value: unknown): value is unknown => validator(value)
    await expect(getJson('/api/v1/test', wrappedValidator)).rejects.toMatchObject({ code: 'invalid_response' })
  })

  it('propagates caller cancellation and removes its listener', async () => {
    const caller = new AbortController()
    const removeListener = vi.spyOn(caller.signal, 'removeEventListener')
    vi.stubGlobal('fetch', vi.fn((_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => {
        reject(init.signal?.reason instanceof Error ? init.signal.reason : new DOMException('Aborted', 'AbortError'))
      }, { once: true })
    })))
    const request = getJson('/api/v1/status', envelopeValidators.status, { signal: caller.signal })
    caller.abort(new DOMException('Query was cancelled.', 'AbortError'))
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
    await expect(request).rejects.not.toBeInstanceOf(ApiError)
    expect(removeListener).toHaveBeenCalled()
  })

  it('turns the client deadline into a request timeout', async () => {
    vi.stubGlobal('fetch', vi.fn((_input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => {
        reject(init.signal?.reason instanceof Error ? init.signal.reason : new DOMException('Aborted', 'AbortError'))
      }, { once: true })
    })))
    await expect(getJson('/api/v1/status', envelopeValidators.status, { timeoutMs: 5 })).rejects.toMatchObject({
      status: 408, code: 'request_timeout', publicMessage: 'The observatory took too long to respond.',
    })
  })

  it('retries only transient failures and only once', () => {
    for (const status of [0, 408, 429, 500, 503, 599]) {
      expect(shouldRetryRequest(0, new ApiError(status, 'temporary', 'Temporary'))).toBe(true)
    }
    for (const status of [400, 404, 600]) {
      expect(shouldRetryRequest(0, new ApiError(status, 'permanent', 'Permanent'))).toBe(false)
    }
    expect(shouldRetryRequest(0, new ApiError(500, 'invalid_response', 'Invalid'))).toBe(false)
    expect(shouldRetryRequest(1, new ApiError(503, 'temporary', 'Temporary'))).toBe(false)
    expect(shouldRetryRequest(0, new DOMException('Cancelled', 'AbortError'))).toBe(false)
  })
})
