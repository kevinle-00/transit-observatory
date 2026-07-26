import type { AlertEnvelope, CollectionEnvelope, Line, StatusEnvelope } from './contracts'

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

function isNullableNumber(value: unknown): value is number | null {
  return value === null || isFiniteNumber(value)
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
  return isRecord(value) && isCount(value.position) &&
    isOptional(value, 'starts_at', isTimestamp) && isOptional(value, 'ends_at', isTimestamp)
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
    isOptional(value, 'latitude', isFiniteNumber) && isOptional(value, 'longitude', isFiniteNumber) &&
    isOptional(value, 'wheelchair_boarding', isCount)
}

function isAlert(value: unknown): boolean {
  return isRecord(value) && isCount(value.id) && isString(value.source_url) && isString(value.source_entity_id) &&
    isCount(value.revision_id) && isCount(value.revision_number) &&
    isOptional(value, 'cause', isString) && isOptional(value, 'effect', isString) && isOptional(value, 'severity', isString) &&
    isArrayOf(value.header, isTranslation) && isArrayOf(value.description, isTranslation) && isArrayOf(value.url, isTranslation) &&
    isTimestamp(value.first_seen_at) && isTimestamp(value.last_seen_at) &&
    isTimestamp(value.revision_first_seen_at) && isTimestamp(value.revision_last_seen_at) &&
    isArrayOf(value.active_periods, isActivePeriod) && isArrayOf(value.routes, isAlertRoute) &&
    isArrayOf(value.stations, isAlertStation)
}

function isLine(value: unknown): boolean {
  return isRecord(value) && isString(value.id) && isString(value.short_name) && isString(value.long_name) &&
    isCount(value.route_type) && isOptional(value, 'color', isString) && isOptional(value, 'text_color', isString) &&
    isBoolean(value.is_replacement_bus) && isCount(value.station_count) && isCount(value.present_alert_count) &&
    isCount(value.current_alert_count) && isCount(value.upcoming_alert_count)
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
    isNullableTimestamp(value.data_as_of) && isNullableNumber(value.data_age_seconds) &&
    isNullableTimestamp(value.check_at) && isNullableNumber(value.check_age_seconds) && isString(value.timestamp_basis) &&
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
      (value.meta.status === 'current' || value.meta.status === 'upcoming')
  },
  status: (value: unknown): value is StatusEnvelope =>
    isRecord(value) && isStatus(value.data) && isStatusMeta(value.meta),
}
