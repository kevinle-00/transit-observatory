import { analyticsMetricLimitations } from '../api/contracts'
import type {
  Alert, AlertDetail, AlertEnvelope, AlertRevision, AnalyticsCollectionEnvelope,
  AnalyticsDetailEnvelope, CollectionEnvelope, DataEnvelope, HistoricalAlertEnvelope,
  Line, LineAnalytics, LineDetail, Station, StationDetail, StatusEnvelope,
} from '../api/contracts'

export const fixedNow = '2026-07-26T10:00:00Z'

export const belgraveLine: Line = {
  id: 'route:belgrave/1', short_name: 'Belgrave', long_name: 'Belgrave', route_type: 2,
  color: '006F45', text_color: 'FFFFFF', is_replacement_bus: false, station_count: 31,
  present_alert_count: 2, current_alert_count: 1, upcoming_alert_count: 1,
}

export const lines: Line[] = [
  belgraveLine,
  {
    id: 'route:frankston', short_name: 'Frankston', long_name: 'Frankston', route_type: 2,
    color: '009B77', text_color: 'FFFFFF', is_replacement_bus: false, station_count: 28,
    present_alert_count: 0, current_alert_count: 0, upcoming_alert_count: 0,
  },
]

export const currentAlert: Alert = {
  id: 41,
  source_url: 'https://transport.example/feed',
  source_entity_id: 'metro-41',
  revision_id: 71,
  revision_number: 2,
  cause: 'TECHNICAL_PROBLEM',
  effect: 'SIGNIFICANT_DELAYS',
  severity: 'WARNING',
  header: [{ text: 'Retards importants', language: 'fr' }, { text: 'Belgrave trains delayed', language: 'en' }],
  description: [{ text: 'Allow an extra 20 minutes while crews attend to a signal fault.', language: 'en-AU' }],
  url: [],
  first_seen_at: '2026-07-26T08:30:00Z',
  last_seen_at: '2026-07-26T09:55:00Z',
  revision_first_seen_at: '2026-07-26T09:00:00Z',
  revision_last_seen_at: '2026-07-26T09:55:00Z',
  active_periods: [{ position: 0, starts_at: '2026-07-26T08:00:00Z' }],
  routes: [{
    source_route_id: 'route:belgrave/1', static_route_id: 'route:belgrave/1', short_name: 'Belgrave',
    long_name: 'Belgrave', route_type: 2, color: '006F45', text_color: 'FFFFFF', is_replacement_bus: false, is_matched: true,
  }],
  stations: [{ source_stop_id: 'stop:richmond', static_station_id: 'station:richmond', name: 'Richmond', is_matched: true }],
}

export const upcomingAlert: Alert = {
  ...currentAlert,
  id: 42,
  source_entity_id: 'metro-upcoming-42',
  header: [{ text: 'Coaches replace trains on Sunday' }],
  description: [{ text: 'Plan extra travel time.' }],
  cause: 'CONSTRUCTION',
  effect: 'NO_SERVICE',
  active_periods: [
    { position: 0, starts_at: '2026-08-02T00:00:00Z', ends_at: '2026-08-02T08:00:00Z' },
    { position: 1, starts_at: '2026-08-09T00:00:00Z', ends_at: '2026-08-09T08:00:00Z' },
  ],
  routes: [{ source_route_id: 'legacy:night-bus', color: 'not-a-color', is_matched: false }],
  stations: [{ source_stop_id: 'legacy-stop-7', is_matched: false }],
}

export const closedAlertRevision: AlertRevision = {
  ...currentAlert,
  revision_id: 72,
  revision_number: 3,
  revision_first_seen_at: '2026-07-26T09:55:00Z',
  revision_last_seen_at: '2026-07-26T10:05:00Z',
  last_seen_at: '2026-07-26T10:05:00Z',
  is_deleted: true,
  closed_at: '2026-07-26T10:05:00Z',
}

export const alertDetail: AlertDetail = {
  id: currentAlert.id,
  source_url: currentAlert.source_url,
  source_entity_id: currentAlert.source_entity_id,
  status: 'historical',
  first_seen_at: currentAlert.first_seen_at,
  last_seen_at: closedAlertRevision.last_seen_at,
  closed_at: '2026-07-26T10:05:00Z',
  revision_count: 3,
  latest_revision: closedAlertRevision,
}

export const richmondStation: Station = {
  id: 'station:richmond',
  name: 'Richmond',
  latitude: -37.8241,
  longitude: 144.9985,
  wheelchair_boarding: 1,
  lines: [belgraveLine],
  present_alert_count: 1,
  current_alert_count: 1,
  upcoming_alert_count: 0,
}

export const stations: Station[] = [richmondStation]

export const lineDetail: LineDetail = {
  line: { ...belgraveLine, station_count: stations.length },
  stations,
  alerts: [currentAlert],
}

export const stationDetail: StationDetail = { station: richmondStation, alerts: [currentAlert] }

const analyticsLine = { ...belgraveLine, station_count: stations.length }

const analyticsBase: Omit<LineAnalytics, 'metric_limitations'> = {
  line: analyticsLine,
  series: [
    { starts_at: '2026-07-20T00:00:00Z', alert_count: 4, completed_episode_sample_count: 2, median_observed_lifetime_seconds: 2700 },
    { starts_at: '2026-07-21T00:00:00Z', alert_count: 1, completed_episode_sample_count: 0 },
  ],
  causes: [{ value: 'TECHNICAL_PROBLEM', count: 3 }, { value: 'UNKNOWN_CAUSE', count: 2 }],
  effects: [{ value: 'SIGNIFICANT_DELAYS', count: 4 }, { value: 'NO_SERVICE', count: 1 }],
}

export const lineAnalytics: LineAnalytics = { ...analyticsBase, metric_limitations: [] }
export const lineAnalyticsDetail: LineAnalytics = { ...analyticsBase, metric_limitations: [...analyticsMetricLimitations] }

const section = {
  freshness: 'fresh' as const,
  data_as_of: '2026-07-26T09:58:00Z',
  data_age_seconds: 120,
  check_at: '2026-07-26T09:59:00Z',
  check_age_seconds: 60,
  timestamp_basis: 'feed_timestamp',
  reasons: [],
  last_applied: null,
  latest_attempt: null,
  recent_failures: [],
}

export const freshStatus: StatusEnvelope = {
  data: {
    generated_at: fixedNow,
    overall_status: 'ok',
    service_alerts: { ...section, counts: { present: 2, current: 1, upcoming: 1, historical: 18 } },
    static_gtfs: { ...section, data_as_of: '2026-07-25T12:00:00Z', counts: { routes: 17, regular_routes: 16, replacement_routes: 1, stops: 244, stations: 222, relations: 260, trips: 2200, stop_times: 49000 } },
  },
  meta: {
    alert_data_max_age_seconds: 600, alert_check_max_age_seconds: 600,
    gtfs_data_max_age_seconds: 172800, gtfs_check_max_age_seconds: 172800,
    alert_run_max_duration_seconds: 300, gtfs_run_max_duration_seconds: 3600,
    future_tolerance_seconds: 30, recent_failure_limit: 3,
  },
}

export const degradedStatus: StatusEnvelope = {
  ...freshStatus,
  data: {
    ...freshStatus.data,
    overall_status: 'degraded',
    service_alerts: { ...freshStatus.data.service_alerts, freshness: 'stale', reasons: ['data_stale', 'recent_failure'] },
  },
}

export const lineEnvelope: CollectionEnvelope<Line> = { data: lines, meta: { count: lines.length } }
export const currentEnvelope: AlertEnvelope = { data: [currentAlert], meta: { count: 1, status: 'current' } }
export const upcomingEnvelope: AlertEnvelope = { data: [upcomingAlert], meta: { count: 1, status: 'upcoming' } }
export const historicalEnvelope: HistoricalAlertEnvelope = {
  data: [closedAlertRevision],
  meta: { count: 1, status: 'historical', total: 18, page: 1, page_size: 25, total_pages: 1 },
}
export const alertDetailEnvelope: DataEnvelope<AlertDetail> = { data: alertDetail }
export const alertRevisionsEnvelope: CollectionEnvelope<AlertRevision> = {
  data: [{ ...currentAlert, is_deleted: false }, closedAlertRevision],
  meta: { count: 2 },
}
export const lineDetailEnvelope: DataEnvelope<LineDetail> = { data: lineDetail }
export const stationEnvelope: CollectionEnvelope<Station> = { data: stations, meta: { count: stations.length } }
export const stationDetailEnvelope: DataEnvelope<StationDetail> = { data: stationDetail }
export const lineAnalyticsEnvelope: AnalyticsCollectionEnvelope = {
  data: [lineAnalytics],
  meta: {
    count: 1,
    from: '2026-07-20T00:00:00Z',
    to: '2026-07-27T00:00:00Z',
    interval: 'day',
    timezone: 'UTC',
    metric_basis: 'continuous_feed_observation_episodes',
  },
}
export const lineAnalyticsDetailEnvelope: AnalyticsDetailEnvelope = {
  data: lineAnalyticsDetail,
  meta: { ...lineAnalyticsEnvelope.meta, count: 1 },
}
