export interface Translation {
  text: string
  language?: string
}

export interface ActivePeriod {
  position: number
  starts_at?: string
  ends_at?: string
}

export interface AlertRoute {
  source_route_id: string
  static_route_id?: string
  short_name?: string
  long_name?: string
  route_type?: number
  color?: string
  text_color?: string
  is_replacement_bus?: boolean
  is_matched: boolean
}

export interface AlertStation {
  source_stop_id: string
  static_station_id?: string
  name?: string
  latitude?: number
  longitude?: number
  wheelchair_boarding?: number
  is_matched: boolean
}

export interface Alert {
  id: number
  source_url: string
  source_entity_id: string
  revision_id: number
  revision_number: number
  cause?: string
  effect?: string
  severity?: string
  header: Translation[]
  description: Translation[]
  url: Translation[]
  first_seen_at: string
  last_seen_at: string
  revision_first_seen_at: string
  revision_last_seen_at: string
  active_periods: ActivePeriod[]
  routes: AlertRoute[]
  stations: AlertStation[]
}

export type AlertStatus = 'present' | 'current' | 'upcoming' | 'historical'
export type AlertLifecycleStatus = 'present' | 'historical'

export interface AlertRevision extends Alert {
  is_deleted: boolean
  closed_at?: string
}

export interface AlertDetail {
  id: number
  source_url: string
  source_entity_id: string
  status: AlertLifecycleStatus
  first_seen_at: string
  last_seen_at: string
  closed_at?: string
  revision_count: number
  latest_revision: AlertRevision
}

export interface Line {
  id: string
  short_name: string
  long_name: string
  route_type: number
  color?: string
  text_color?: string
  is_replacement_bus: boolean
  station_count: number
  present_alert_count: number
  current_alert_count: number
  upcoming_alert_count: number
}

export interface Station {
  id: string
  name: string
  latitude?: number
  longitude?: number
  wheelchair_boarding?: number
  lines: Line[]
  present_alert_count: number
  current_alert_count: number
  upcoming_alert_count: number
}

export interface LineDetail {
  line: Line
  stations: Station[]
  alerts: Alert[]
}

export interface StationDetail {
  station: Station
  alerts: Alert[]
}

export type AnalyticsInterval = 'day' | 'week'

export interface AnalyticsPoint {
  starts_at: string
  alert_count: number
  completed_episode_sample_count: number
  median_observed_lifetime_seconds?: number
}

export interface AnalyticsBreakdown {
  value: string
  count: number
}

export const analyticsMetricLimitations = [
  'An alert is counted once each time it appears. If it disappears and later returns, it is counted again. Counts do not measure incidents or affected passengers.',
  'For alerts that ended, duration runs from the first to the last time the alert appeared. It does not include the time needed to confirm that the alert had ended.',
  'Older alerts use the current line list, which may differ from the lines in service when the alert appeared.',
] as const

export type AnalyticsMetricLimitation = typeof analyticsMetricLimitations[number]

export interface LineAnalytics {
  line: Line
  series: AnalyticsPoint[]
  causes: AnalyticsBreakdown[]
  effects: AnalyticsBreakdown[]
  metric_limitations: AnalyticsMetricLimitation[]
}

export interface AnalyticsMeta {
  count: number
  from: string
  to: string
  interval: AnalyticsInterval
  timezone: 'UTC'
  metric_basis: 'continuous_feed_observation_episodes'
}

export type Freshness = 'fresh' | 'stale' | 'unknown' | 'unavailable'
export type OverallStatus = 'ok' | 'degraded' | 'unavailable'

export interface ArchiveSummary {
  object_key: string
  sha256: string
  bytes: number
  stored_at: string
  created: boolean | null
}

export interface IngestionSummary {
  id: number
  started_at: string
  completed_at: string
  retrieved_at: string | null
  data_as_of: string | null
  archive: ArchiveSummary | null
  item_count: number
  trip_count?: number
  stop_time_count?: number
}

export interface AttemptSummary {
  id: number
  outcome: string
  started_at: string
  completed_at: string | null
  duration_seconds: number
  overdue: boolean
  archive: ArchiveSummary | null
}

export interface FailureSummary {
  id: number
  failed_at: string | null
  stage: string
  code: string
  public_message: string
  archive: ArchiveSummary | null
}

interface StatusSection {
  freshness: Freshness
  data_as_of: string | null
  data_age_seconds: number | null
  check_at: string | null
  check_age_seconds: number | null
  timestamp_basis: string
  reasons: string[]
  last_applied: IngestionSummary | null
  latest_attempt: AttemptSummary | null
  recent_failures: FailureSummary[]
}

export interface Status {
  generated_at: string
  overall_status: OverallStatus
  service_alerts: StatusSection & {
    counts: { present: number; current: number; upcoming: number; historical: number }
  }
  static_gtfs: StatusSection & {
    counts: {
      routes: number
      regular_routes: number
      replacement_routes: number
      stops: number
      stations: number
      relations: number
      trips: number
      stop_times: number
    }
  }
}

export interface StatusMeta {
  alert_data_max_age_seconds: number
  alert_check_max_age_seconds: number
  gtfs_data_max_age_seconds: number
  gtfs_check_max_age_seconds: number
  alert_run_max_duration_seconds: number
  gtfs_run_max_duration_seconds: number
  future_tolerance_seconds: number
  recent_failure_limit: number
}

export interface CollectionEnvelope<T> {
  data: T[]
  meta: { count: number }
}

export interface AlertEnvelope {
  data: Alert[]
  meta: { count: number; status: AlertStatus }
}

export interface HistoricalAlertEnvelope {
  data: Alert[]
  meta: {
    count: number
    status: 'historical'
    total: number
    page: number
    page_size: number
    total_pages: number
  }
}

export interface DataEnvelope<T> {
  data: T
}

export interface AnalyticsCollectionEnvelope {
  data: LineAnalytics[]
  meta: AnalyticsMeta
}

export interface AnalyticsDetailEnvelope {
  data: LineAnalytics
  meta: AnalyticsMeta & { count: 1 }
}

export interface StatusEnvelope {
  data: Status
  meta: StatusMeta
}

export interface ErrorEnvelope {
  error: { code: string; message: string }
}
