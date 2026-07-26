import { queryOptions } from '@tanstack/react-query'
import { getJson, shouldRetryRequest } from './client'
import type {
  AlertEnvelope, AlertStatus, AnalyticsCollectionEnvelope, AnalyticsDetailEnvelope, AnalyticsInterval,
  CollectionEnvelope, DataEnvelope, HistoricalAlertEnvelope, Line, LineDetail, Station, StationDetail,
  AlertDetail, AlertRevision, StatusEnvelope,
} from './contracts'
import { envelopeValidators } from './validation'

const minute = 60_000

type Nullable<T> = { [K in keyof T]-?: T[K] | null }

export interface AlertListInput {
  status: Exclude<AlertStatus, 'historical'>
  lineId?: string
  stationId?: string
  cause?: string
  effect?: string
  from?: string
  to?: string
}

export interface HistoricalAlertInput {
  lineId?: string
  stationId?: string
  cause?: string
  effect?: string
  from?: string
  to?: string
  page?: number
  pageSize?: number
}

export interface StationListInput {
  q?: string
  lineId?: string
}

export interface LineAnalyticsInput {
  from?: string
  to?: string
  interval?: AnalyticsInterval
  includeReplacementBus?: boolean
}

export interface LineAnalyticsDetailInput {
  from?: string
  to?: string
  interval?: AnalyticsInterval
}

function setOptional(params: URLSearchParams, name: string, value: string | null): void {
  if (value !== null) params.set(name, value)
}

function normalizedAlertInput(input: AlertListInput) {
  return {
    status: input.status,
    lineId: input.lineId ?? null,
    stationId: input.stationId ?? null,
    cause: input.cause ?? null,
    effect: input.effect ?? null,
    from: input.from ?? null,
    to: input.to ?? null,
  } as const
}

function alertParams(input: {
  status: AlertStatus
  lineId: string | null
  stationId: string | null
  cause: string | null
  effect: string | null
  from: string | null
  to: string | null
}): URLSearchParams {
  const params = new URLSearchParams({ status: input.status })
  setOptional(params, 'line_id', input.lineId)
  setOptional(params, 'station_id', input.stationId)
  setOptional(params, 'cause', input.cause)
  setOptional(params, 'effect', input.effect)
  setOptional(params, 'from', input.from)
  setOptional(params, 'to', input.to)
  return params
}

export const statusQuery = queryOptions<StatusEnvelope>({
  queryKey: ['status'],
  queryFn: ({ signal }) => getJson<StatusEnvelope>('/api/v1/status', envelopeValidators.status, { signal }),
  refetchInterval: minute,
  retry: shouldRetryRequest,
})

export function linesQueryOptions(includeReplacementBus = false) {
  const input = { includeReplacementBus }
  const params = includeReplacementBus ? '?include_replacement_bus=true' : ''
  return queryOptions<CollectionEnvelope<Line>>({
    queryKey: ['lines', input],
    queryFn: ({ signal }) => getJson<CollectionEnvelope<Line>>(`/api/v1/lines${params}`, envelopeValidators.lines, { signal }),
    staleTime: 30 * minute,
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}

export const linesQuery = linesQueryOptions()

export function alertListQuery(input: AlertListInput) {
  const normalized = normalizedAlertInput(input)
  const params = alertParams(normalized)
  return queryOptions<AlertEnvelope>({
    queryKey: ['alerts', normalized],
    queryFn: ({ signal }) => getJson<AlertEnvelope>(
      `/api/v1/alerts?${params.toString()}`,
      envelopeValidators.alertsForStatus(normalized.status),
      { signal },
    ),
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}

export function alertsQuery(status: 'current' | 'upcoming', lineId: string | null) {
  return {
    ...alertListQuery({ status, ...(lineId === null ? {} : { lineId }) }),
    queryKey: ['alerts', status, lineId] as const,
  }
}

export function historicalAlertsQuery(input: HistoricalAlertInput = {}) {
  const normalized = {
    status: 'historical' as const,
    lineId: input.lineId ?? null,
    stationId: input.stationId ?? null,
    cause: input.cause ?? null,
    effect: input.effect ?? null,
    from: input.from ?? null,
    to: input.to ?? null,
    page: input.page ?? 1,
    pageSize: input.pageSize ?? 25,
  }
  const params = alertParams(normalized)
  params.set('page', String(normalized.page))
  params.set('page_size', String(normalized.pageSize))
  return queryOptions<HistoricalAlertEnvelope>({
    queryKey: ['alerts', normalized],
    queryFn: ({ signal }) => getJson<HistoricalAlertEnvelope>(
      `/api/v1/alerts?${params.toString()}`,
      envelopeValidators.historicalAlerts,
      { signal },
    ),
    placeholderData: (previous) => previous,
    refetchInterval: 5 * minute,
    retry: shouldRetryRequest,
  })
}

export function alertDetailQuery(id: number) {
  return queryOptions<DataEnvelope<AlertDetail>>({
    queryKey: ['alert', { id }],
    queryFn: ({ signal }) => getJson<DataEnvelope<AlertDetail>>(
      `/api/v1/alerts/${encodeURIComponent(String(id))}`,
      envelopeValidators.alertDetail(id),
      { signal },
    ),
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}

export function alertRevisionsQuery(id: number) {
  return queryOptions<CollectionEnvelope<AlertRevision>>({
    queryKey: ['alert-revisions', { id }],
    queryFn: ({ signal }) => getJson<CollectionEnvelope<AlertRevision>>(
      `/api/v1/alerts/${encodeURIComponent(String(id))}/revisions`,
      envelopeValidators.alertRevisions(id),
      { signal },
    ),
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}

export function lineDetailQuery(id: string) {
  return queryOptions<DataEnvelope<LineDetail>>({
    queryKey: ['line', { id }],
    queryFn: ({ signal }) => getJson<DataEnvelope<LineDetail>>(
      `/api/v1/lines/${encodeURIComponent(id)}`,
      envelopeValidators.lineDetail(id),
      { signal },
    ),
    staleTime: 30 * minute,
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}

export function stationsQuery(input: StationListInput = {}) {
  const normalized: Nullable<StationListInput> = { q: input.q ?? null, lineId: input.lineId ?? null }
  const params = new URLSearchParams()
  setOptional(params, 'q', normalized.q)
  setOptional(params, 'line_id', normalized.lineId)
  const query = params.size === 0 ? '' : `?${params.toString()}`
  return queryOptions<CollectionEnvelope<Station>>({
    queryKey: ['stations', normalized],
    queryFn: ({ signal }) => getJson<CollectionEnvelope<Station>>(`/api/v1/stations${query}`, envelopeValidators.stations, { signal }),
    staleTime: 30 * minute,
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}

export function stationDetailQuery(id: string) {
  return queryOptions<DataEnvelope<StationDetail>>({
    queryKey: ['station', { id }],
    queryFn: ({ signal }) => getJson<DataEnvelope<StationDetail>>(
      `/api/v1/stations/${encodeURIComponent(id)}`,
      envelopeValidators.stationDetail(id),
      { signal },
    ),
    staleTime: 30 * minute,
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}

export function lineAnalyticsQuery(input: LineAnalyticsInput = {}) {
  const normalized = {
    from: input.from ?? null,
    to: input.to ?? null,
    interval: input.interval ?? null,
    includeReplacementBus: input.includeReplacementBus ?? false,
  }
  const params = analyticsParams(normalized)
  if (normalized.includeReplacementBus) params.set('include_replacement_bus', 'true')
  const query = params.size === 0 ? '' : `?${params.toString()}`
  return queryOptions<AnalyticsCollectionEnvelope>({
    queryKey: ['line-analytics', normalized],
    queryFn: ({ signal }) => getJson<AnalyticsCollectionEnvelope>(`/api/v1/analytics/lines${query}`, envelopeValidators.lineAnalytics, { signal }),
    refetchInterval: 5 * minute,
    retry: shouldRetryRequest,
  })
}

function analyticsParams(input: { from: string | null; to: string | null; interval: AnalyticsInterval | null }): URLSearchParams {
  const params = new URLSearchParams()
  setOptional(params, 'from', input.from)
  setOptional(params, 'to', input.to)
  setOptional(params, 'interval', input.interval)
  return params
}

export function lineAnalyticsDetailQuery(id: string, input: LineAnalyticsDetailInput = {}) {
  const normalized = { id, from: input.from ?? null, to: input.to ?? null, interval: input.interval ?? null }
  const params = analyticsParams(normalized)
  const query = params.size === 0 ? '' : `?${params.toString()}`
  return queryOptions<AnalyticsDetailEnvelope>({
    queryKey: ['line-analytics-detail', normalized],
    queryFn: ({ signal }) => getJson<AnalyticsDetailEnvelope>(
      `/api/v1/analytics/lines/${encodeURIComponent(id)}${query}`,
      envelopeValidators.lineAnalyticsDetail(id),
      { signal },
    ),
    refetchInterval: 5 * minute,
    retry: shouldRetryRequest,
  })
}
