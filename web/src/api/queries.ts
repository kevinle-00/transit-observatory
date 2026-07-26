import { queryOptions } from '@tanstack/react-query'
import { getJson, shouldRetryRequest } from './client'
import type { AlertEnvelope, CollectionEnvelope, Line, StatusEnvelope } from './contracts'
import { envelopeValidators } from './validation'

const minute = 60_000

export const statusQuery = queryOptions<StatusEnvelope>({
  queryKey: ['status'],
  queryFn: ({ signal }) => getJson<StatusEnvelope>('/api/v1/status', envelopeValidators.status, { signal }),
  refetchInterval: minute,
  retry: shouldRetryRequest,
})

export const linesQuery = queryOptions<CollectionEnvelope<Line>>({
  queryKey: ['lines'],
  queryFn: ({ signal }) => getJson<CollectionEnvelope<Line>>('/api/v1/lines', envelopeValidators.lines, { signal }),
  staleTime: 30 * minute,
  retry: shouldRetryRequest,
})

export function alertsQuery(status: 'current' | 'upcoming', lineId: string | null) {
  const params = new URLSearchParams({ status })
  if (lineId) params.set('line_id', lineId)
  return queryOptions<AlertEnvelope>({
    queryKey: ['alerts', status, lineId],
    queryFn: ({ signal }) => getJson<AlertEnvelope>(`/api/v1/alerts?${params.toString()}`, envelopeValidators.alerts, { signal }),
    refetchInterval: minute,
    retry: shouldRetryRequest,
  })
}
