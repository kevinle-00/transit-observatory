import { analyticsMetricLimitations } from './contracts'
import type {
  Alert, AlertDetail, AlertEnvelope, AlertRevision, AlertStatus, AnalyticsCollectionEnvelope, AnalyticsPoint,
  AnalyticsDetailEnvelope, CollectionEnvelope, DataEnvelope, HistoricalAlertEnvelope,
  Line, LineAnalytics, LineDetail, Station, StationDetail, StatusEnvelope,
} from './contracts'

type RecordValue = Record<string, unknown>

function isRecord(value: unknown): value is RecordValue {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isString(value: unknown): value is string {
  return typeof value === 'string'
}

function isBoolean(value: unknown): value is boolean {
  return typeof value === 'boolean'
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isCount(value: unknown): value is number {
  return isFiniteNumber(value) && Number.isInteger(value) && value >= 0
}

function isPositiveInteger(value: unknown): value is number {
  return isCount(value) && value > 0
}

function isTimestamp(value: unknown): value is string {
  if (!isString(value)) return false
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(value)
  if (!match) return false
  const [, year, month, day, hour, minute, second, offsetHour = '00', offsetMinute = '00'] = match
  const yearNumber = Number(year)
  const monthNumber = Number(month)
  const dayNumber = Number(day)
  if (monthNumber < 1 || monthNumber > 12 || dayNumber < 1 ||
      dayNumber > new Date(Date.UTC(yearNumber, monthNumber, 0)).getUTCDate()) return false
  if (Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59 ||
      Number(offsetHour) > 23 || Number(offsetMinute) > 59) return false
  return Number.isFinite(Date.parse(value))
}

function isNullableTimestamp(value: unknown): value is string | null {
  return value === null || isTimestamp(value)
}

function isNullableNonNegativeNumber(value: unknown): value is number | null {
  return value === null || isFiniteNumber(value) && value >= 0
}

function isOptional(value: RecordValue, key: string, validate: (item: unknown) => boolean): boolean {
  return !(key in value) || validate(value[key])
}

function isArrayOf(value: unknown, validate: (item: unknown) => boolean): boolean {
  return Array.isArray(value) && value.every(validate)
}

function isTranslation(value: unknown): boolean {
  return isRecord(value) && isString(value.text) && isOptional(value, 'language', isString)
}

function isActivePeriod(value: unknown): boolean {
  if (!isRecord(value) || !isCount(value.position)) return false
  const startsAt = value.starts_at
  const endsAt = value.ends_at
  if (startsAt !== undefined && !isTimestamp(startsAt)) return false
  if (endsAt !== undefined && !isTimestamp(endsAt)) return false
  return startsAt === undefined || endsAt === undefined || new Date(startsAt).getTime() < new Date(endsAt).getTime()
}

function isAlertRoute(value: unknown): boolean {
  return isRecord(value) && isString(value.source_route_id) && isBoolean(value.is_matched) &&
    isOptional(value, 'static_route_id', isString) && isOptional(value, 'short_name', isString) &&
    isOptional(value, 'long_name', isString) && isOptional(value, 'route_type', isCount) &&
    isOptional(value, 'color', isString) && isOptional(value, 'text_color', isString) &&
    isOptional(value, 'is_replacement_bus', isBoolean)
}

function isAlertStation(value: unknown): boolean {
  return isRecord(value) && isString(value.source_stop_id) && isBoolean(value.is_matched) &&
    isOptional(value, 'static_station_id', isString) && isOptional(value, 'name', isString) &&
    isOptional(value, 'latitude', (item) => isFiniteNumber(item) && item >= -90 && item <= 90) &&
    isOptional(value, 'longitude', (item) => isFiniteNumber(item) && item >= -180 && item <= 180) &&
    isOptional(value, 'wheelchair_boarding', (item) => isCount(item) && item <= 2)
}

function isAlert(value: unknown): value is Alert {
  return isRecord(value) && isPositiveInteger(value.id) && isString(value.source_url) && isString(value.source_entity_id) &&
    isPositiveInteger(value.revision_id) && isPositiveInteger(value.revision_number) &&
    isOptional(value, 'cause', isString) && isOptional(value, 'effect', isString) && isOptional(value, 'severity', isString) &&
    isArrayOf(value.header, isTranslation) && isArrayOf(value.description, isTranslation) && isArrayOf(value.url, isTranslation) &&
    isTimestamp(value.first_seen_at) && isTimestamp(value.last_seen_at) &&
    isTimestamp(value.revision_first_seen_at) && isTimestamp(value.revision_last_seen_at) &&
    isArrayOf(value.active_periods, isActivePeriod) && isArrayOf(value.routes, isAlertRoute) &&
    isArrayOf(value.stations, isAlertStation)
}

function isAlertRevision(value: unknown): value is AlertRevision {
  return isAlert(value) && isRecord(value) && isBoolean(value.is_deleted) && isOptional(value, 'closed_at', isTimestamp)
}

function isAlertDetail(value: unknown): value is AlertDetail {
  return isRecord(value) && isPositiveInteger(value.id) && isString(value.source_url) &&
    isString(value.source_entity_id) && (value.status === 'present' || value.status === 'historical') && isTimestamp(value.first_seen_at) &&
    isTimestamp(value.last_seen_at) && isOptional(value, 'closed_at', isTimestamp) &&
    isPositiveInteger(value.revision_count) && isAlertRevision(value.latest_revision) &&
    value.latest_revision.id === value.id && value.latest_revision.revision_number <= value.revision_count
}

function isLine(value: unknown): value is Line {
  return isRecord(value) && isString(value.id) && isString(value.short_name) && isString(value.long_name) &&
    isCount(value.route_type) && isOptional(value, 'color', isString) && isOptional(value, 'text_color', isString) &&
    isBoolean(value.is_replacement_bus) && isCount(value.station_count) && isCount(value.present_alert_count) &&
    isCount(value.current_alert_count) && isCount(value.upcoming_alert_count) &&
    value.current_alert_count <= value.present_alert_count && value.upcoming_alert_count <= value.present_alert_count
}

function isStation(value: unknown): value is Station {
  return isRecord(value) && isString(value.id) && isString(value.name) &&
    isOptional(value, 'latitude', (item) => isFiniteNumber(item) && item >= -90 && item <= 90) &&
    isOptional(value, 'longitude', (item) => isFiniteNumber(item) && item >= -180 && item <= 180) &&
    isOptional(value, 'wheelchair_boarding', (item) => isCount(item) && item <= 2) && isArrayOf(value.lines, isLine) &&
    isCount(value.present_alert_count) && isCount(value.current_alert_count) && isCount(value.upcoming_alert_count) &&
    value.current_alert_count <= value.present_alert_count && value.upcoming_alert_count <= value.present_alert_count
}

function isLineDetail(value: unknown): value is LineDetail {
  return isRecord(value) && isLine(value.line) && Array.isArray(value.stations) &&
    value.stations.every(isStation) && isArrayOf(value.alerts, isAlert) &&
    value.line.station_count === value.stations.length
}

function isStationDetail(value: unknown): value is StationDetail {
  return isRecord(value) && isStation(value.station) && isArrayOf(value.alerts, isAlert)
}

function isAnalyticsPoint(value: unknown): boolean {
  return isRecord(value) && isTimestamp(value.starts_at) &&
    isCount(value.alert_count) && isCount(value.completed_episode_sample_count) &&
    value.completed_episode_sample_count <= value.alert_count &&
    isOptional(value, 'median_observed_lifetime_seconds', (item) => isFiniteNumber(item) && item >= 0)
}

function isAnalyticsBreakdown(value: unknown): boolean {
  return isRecord(value) && isString(value.value) && isCount(value.count)
}

function isLineAnalytics(value: unknown, detail: boolean): value is LineAnalytics {
  if (!isRecord(value) || !isLine(value.line) || !isArrayOf(value.series, isAnalyticsPoint) ||
      !isArrayOf(value.causes, isAnalyticsBreakdown) || !isArrayOf(value.effects, isAnalyticsBreakdown) ||
      !Array.isArray(value.metric_limitations)) return false
  const starts = (value.series as AnalyticsPoint[]).map((point) => new Date(point.starts_at).getTime())
  if (starts.some((start, index) => index > 0 && start <= (starts[index - 1] ?? -Infinity))) return false
  if (!detail) return value.metric_limitations.length === 0
  return value.metric_limitations.length === analyticsMetricLimitations.length &&
    value.metric_limitations.every((item, index) => item === analyticsMetricLimitations[index])
}

function isAnalyticsMeta(value: unknown, count: number): boolean {
  return isRecord(value) && value.count === count && isTimestamp(value.from) && isTimestamp(value.to) &&
    new Date(value.from).getTime() < new Date(value.to).getTime() &&
    (value.interval === 'day' || value.interval === 'week') && value.timezone === 'UTC' &&
    value.metric_basis === 'continuous_feed_observation_episodes'
}

function isArchive(value: unknown): boolean {
  return isRecord(value) && isString(value.object_key) && isString(value.sha256) && isCount(value.bytes) &&
    isTimestamp(value.stored_at) && (value.created === null || isBoolean(value.created))
}

function isNullableArchive(value: unknown): boolean {
  return value === null || isArchive(value)
}

function isIngestion(value: unknown): boolean {
  return isRecord(value) && isCount(value.id) && isTimestamp(value.started_at) && isTimestamp(value.completed_at) &&
    isNullableTimestamp(value.retrieved_at) && isNullableTimestamp(value.data_as_of) && isNullableArchive(value.archive) &&
    isCount(value.item_count) && isOptional(value, 'trip_count', isCount) && isOptional(value, 'stop_time_count', isCount)
}

function isAttempt(value: unknown): boolean {
  return isRecord(value) && isCount(value.id) && isString(value.outcome) && isTimestamp(value.started_at) &&
    isNullableTimestamp(value.completed_at) && isFiniteNumber(value.duration_seconds) && value.duration_seconds >= 0 &&
    isBoolean(value.overdue) && isNullableArchive(value.archive)
}

function isFailure(value: unknown): boolean {
  return isRecord(value) && isCount(value.id) && isNullableTimestamp(value.failed_at) && isString(value.stage) &&
    isString(value.code) && isString(value.public_message) && isNullableArchive(value.archive)
}

function isStatusSection(value: unknown): value is RecordValue {
  return isRecord(value) && ['fresh', 'stale', 'unknown', 'unavailable'].includes(String(value.freshness)) &&
    isNullableTimestamp(value.data_as_of) && isNullableNonNegativeNumber(value.data_age_seconds) &&
    isNullableTimestamp(value.check_at) && isNullableNonNegativeNumber(value.check_age_seconds) && isString(value.timestamp_basis) &&
    isArrayOf(value.reasons, isString) && (value.last_applied === null || isIngestion(value.last_applied)) &&
    (value.latest_attempt === null || isAttempt(value.latest_attempt)) && isArrayOf(value.recent_failures, isFailure)
}

function hasCounts(value: unknown, names: string[]): boolean {
  return isRecord(value) && names.every((name) => isCount(value[name]))
}

function isStatus(value: unknown): boolean {
  if (!isRecord(value) || !isTimestamp(value.generated_at) || !['ok', 'degraded', 'unavailable'].includes(String(value.overall_status)) ||
      !isStatusSection(value.service_alerts) || !isStatusSection(value.static_gtfs)) return false
  return hasCounts(value.service_alerts.counts, ['present', 'current', 'upcoming', 'historical']) &&
    hasCounts(value.static_gtfs.counts, ['routes', 'regular_routes', 'replacement_routes', 'stops', 'stations', 'relations', 'trips', 'stop_times'])
}

function isStatusMeta(value: unknown): boolean {
  return isRecord(value) && [
    'alert_data_max_age_seconds', 'alert_check_max_age_seconds', 'gtfs_data_max_age_seconds',
    'gtfs_check_max_age_seconds', 'alert_run_max_duration_seconds', 'gtfs_run_max_duration_seconds',
    'future_tolerance_seconds',
  ].every((name) => isFiniteNumber(value[name]) && value[name] >= 0) && isCount(value.recent_failure_limit)
}

export const envelopeValidators = {
  lines: (value: unknown): value is CollectionEnvelope<Line> => {
    if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) return false
    return value.data.every(isLine) && isCount(value.meta.count) && value.meta.count === value.data.length
  },
  alerts: (value: unknown): value is AlertEnvelope => {
    if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) return false
    return value.data.every(isAlert) && isCount(value.meta.count) && value.meta.count === value.data.length &&
      (value.meta.status === 'present' || value.meta.status === 'current' || value.meta.status === 'upcoming')
  },
  alertsForStatus: (status: Exclude<AlertStatus, 'historical'>) => (value: unknown): value is AlertEnvelope =>
    envelopeValidators.alerts(value) && value.meta.status === status,
  historicalAlerts: (value: unknown): value is HistoricalAlertEnvelope => {
    if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) return false
    return value.data.every(isAlert) && value.meta.status === 'historical' &&
      isCount(value.meta.count) && value.meta.count === value.data.length && isCount(value.meta.total) &&
      value.meta.total >= value.meta.count && isPositiveInteger(value.meta.page) &&
      isPositiveInteger(value.meta.page_size) && isCount(value.meta.total_pages) &&
      value.meta.total_pages === Math.ceil(value.meta.total / value.meta.page_size)
  },
  alertDetail: (id?: number) => (value: unknown): value is DataEnvelope<AlertDetail> =>
    isRecord(value) && isAlertDetail(value.data) && (id === undefined || value.data.id === id),
  alertRevisions: (id?: number) => (value: unknown): value is CollectionEnvelope<AlertRevision> => {
    if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta) ||
        !value.data.every(isAlertRevision) || !isCount(value.meta.count) || value.meta.count !== value.data.length) return false
    return id === undefined || value.data.every((revision) => revision.id === id)
  },
  lineDetail: (id?: string) => (value: unknown): value is DataEnvelope<LineDetail> =>
    isRecord(value) && isLineDetail(value.data) && (id === undefined || value.data.line.id === id),
  stations: (value: unknown): value is CollectionEnvelope<Station> => {
    if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) return false
    return value.data.every(isStation) && isCount(value.meta.count) && value.meta.count === value.data.length
  },
  stationDetail: (id?: string) => (value: unknown): value is DataEnvelope<StationDetail> =>
    isRecord(value) && isStationDetail(value.data) && (id === undefined || value.data.station.id === id),
  lineAnalytics: (value: unknown): value is AnalyticsCollectionEnvelope => {
    if (!isRecord(value) || !Array.isArray(value.data) || !value.data.every((item) => isLineAnalytics(item, false))) return false
    return isAnalyticsMeta(value.meta, value.data.length)
  },
  lineAnalyticsDetail: (id?: string) => (value: unknown): value is AnalyticsDetailEnvelope =>
    isRecord(value) && isLineAnalytics(value.data, true) && isAnalyticsMeta(value.meta, 1) &&
    (id === undefined || value.data.line.id === id),
  status: (value: unknown): value is StatusEnvelope =>
    isRecord(value) && isStatus(value.data) && isStatusMeta(value.meta),
}
