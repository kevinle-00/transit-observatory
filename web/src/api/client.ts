import { apiBaseUrl } from '../app/env'
import type { ErrorEnvelope } from './contracts'

const REQUEST_TIMEOUT_MS = 12_000

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    public readonly publicMessage: string,
  ) {
    super(publicMessage)
    this.name = 'ApiError'
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isErrorEnvelope(value: unknown): value is ErrorEnvelope {
  return isRecord(value) && isRecord(value.error) && typeof value.error.code === 'string' && typeof value.error.message === 'string'
}

interface RequestOptions {
  signal?: AbortSignal
  timeoutMs?: number
}

function abortReason(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException('The request was aborted.', 'AbortError')
}

function isTimeout(signal: AbortSignal): boolean {
  return signal.reason instanceof DOMException && signal.reason.name === 'TimeoutError'
}

export async function getJson<T>(path: string, validate: (value: unknown) => value is T, options: RequestOptions = {}): Promise<T> {
  const controller = new AbortController()
  const onCallerAbort = () => controller.abort(options.signal ? abortReason(options.signal) : undefined)
  if (options.signal?.aborted) throw abortReason(options.signal)
  options.signal?.addEventListener('abort', onCallerAbort, { once: true })
  const timeout = window.setTimeout(() => {
    controller.abort(new DOMException('The request timed out.', 'TimeoutError'))
  }, options.timeoutMs ?? REQUEST_TIMEOUT_MS)

  try {
    let response: Response
    try {
      response = await fetch(`${apiBaseUrl}${path}`, { headers: { Accept: 'application/json' }, signal: controller.signal })
    } catch (error) {
      if (isTimeout(controller.signal)) {
        throw new ApiError(408, 'request_timeout', 'The observatory took too long to respond.')
      }
      if (options.signal?.aborted) throw error
      throw new ApiError(0, 'network_error', 'The observatory could not be reached.')
    }
    let body: unknown
    try {
      body = await response.json()
    } catch (error) {
      if (isTimeout(controller.signal)) throw new ApiError(408, 'request_timeout', 'The observatory took too long to respond.')
      if (options.signal?.aborted) throw error
      throw new ApiError(response.status, 'invalid_response', 'The observatory returned an unreadable response.')
    }
    if (isTimeout(controller.signal)) throw new ApiError(408, 'request_timeout', 'The observatory took too long to respond.')
    if (options.signal?.aborted) throw abortReason(options.signal)
    if (!response.ok) {
      if (isErrorEnvelope(body)) throw new ApiError(response.status, body.error.code, body.error.message)
      throw new ApiError(response.status, 'request_failed', 'The observatory could not complete this request.')
    }
    if (!validate(body)) throw new ApiError(response.status, 'invalid_response', 'The observatory returned an unexpected response.')
    return body
  } finally {
    window.clearTimeout(timeout)
    options.signal?.removeEventListener('abort', onCallerAbort)
  }
}

export function shouldRetryRequest(failureCount: number, error: unknown): boolean {
  if (failureCount >= 1 || !(error instanceof ApiError) || error.code === 'invalid_response') return false
  return error.status === 0 || error.status === 408 || error.status === 429 || error.status >= 500 && error.status <= 599
}
