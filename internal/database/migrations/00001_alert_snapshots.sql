-- +goose Up
CREATE TABLE ingestion_runs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feed_type TEXT NOT NULL CHECK (feed_type IN ('service_alerts')),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    source_url TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    retrieved_at TIMESTAMPTZ,
    feed_timestamp TIMESTAMPTZ,
    http_status INTEGER,
    content_type TEXT,
    payload_bytes BIGINT,
    content_sha256 CHAR(64),
    entity_count INTEGER,
    alert_count INTEGER,
    error_message TEXT
);

CREATE INDEX ingestion_runs_feed_started_idx
    ON ingestion_runs (feed_type, started_at DESC);

CREATE INDEX ingestion_runs_status_idx
    ON ingestion_runs (status) WHERE status <> 'succeeded';

CREATE TABLE service_alert_snapshots (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ingestion_run_id BIGINT NOT NULL REFERENCES ingestion_runs (id) ON DELETE CASCADE,
    source_entity_id TEXT NOT NULL,
    is_deleted BOOLEAN NOT NULL,
    cause TEXT,
    effect TEXT,
    severity TEXT,
    header JSONB NOT NULL,
    description JSONB NOT NULL,
    url JSONB NOT NULL,
    unknown_fields_bytes INTEGER NOT NULL CHECK (unknown_fields_bytes >= 0),
    UNIQUE (ingestion_run_id, source_entity_id)
);

CREATE INDEX service_alert_snapshots_source_entity_idx
    ON service_alert_snapshots (source_entity_id);

CREATE TABLE alert_snapshot_active_periods (
    alert_snapshot_id BIGINT NOT NULL REFERENCES service_alert_snapshots (id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    PRIMARY KEY (alert_snapshot_id, position)
);

CREATE TABLE alert_snapshot_informed_entities (
    alert_snapshot_id BIGINT NOT NULL REFERENCES service_alert_snapshots (id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    agency_id TEXT,
    route_id TEXT,
    route_type INTEGER,
    stop_id TEXT,
    direction_id BIGINT,
    trip_id TEXT,
    trip_route_id TEXT,
    trip_start_time TEXT,
    trip_start_date TEXT,
    trip_direction_id BIGINT,
    trip_schedule_relationship TEXT,
    PRIMARY KEY (alert_snapshot_id, position)
);

CREATE INDEX alert_snapshot_entities_route_idx
    ON alert_snapshot_informed_entities (route_id) WHERE route_id IS NOT NULL;

CREATE INDEX alert_snapshot_entities_stop_idx
    ON alert_snapshot_informed_entities (stop_id) WHERE stop_id IS NOT NULL;

-- +goose Down
DROP TABLE alert_snapshot_informed_entities;
DROP TABLE alert_snapshot_active_periods;
DROP TABLE service_alert_snapshots;
DROP TABLE ingestion_runs;
