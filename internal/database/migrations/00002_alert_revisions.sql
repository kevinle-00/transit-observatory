-- +goose Up
ALTER TABLE ingestion_runs
    DROP CONSTRAINT ingestion_runs_status_check;

ALTER TABLE ingestion_runs
    ADD CONSTRAINT ingestion_runs_status_check
        CHECK (status IN ('running', 'succeeded', 'failed', 'skipped')),
    ADD COLUMN skip_reason TEXT,
    ADD COLUMN alert_reconciliation_applied BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE service_alert_snapshots
    ADD COLUMN unknown_fields_sha256 CHAR(64);

CREATE INDEX ingestion_runs_source_success_idx
    ON ingestion_runs (source_url, id DESC) WHERE status = 'succeeded';

CREATE TABLE service_alerts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_url TEXT NOT NULL,
    source_entity_id TEXT NOT NULL CHECK (source_entity_id <> ''),
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    first_seen_run_id BIGINT NOT NULL REFERENCES ingestion_runs (id),
    last_seen_run_id BIGINT NOT NULL REFERENCES ingestion_runs (id),
    is_present BOOLEAN NOT NULL DEFAULT true,
    closed_at TIMESTAMPTZ,
    current_revision_id BIGINT,
    UNIQUE (source_url, source_entity_id)
);

CREATE INDEX service_alerts_present_idx
    ON service_alerts (is_present, last_seen_at DESC);

CREATE TABLE service_alert_revisions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    service_alert_id BIGINT NOT NULL REFERENCES service_alerts (id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL CHECK (revision_number > 0),
    content_sha256 CHAR(64) NOT NULL,
    is_deleted BOOLEAN NOT NULL,
    cause TEXT,
    effect TEXT,
    severity TEXT,
    header JSONB NOT NULL,
    description JSONB NOT NULL,
    url JSONB NOT NULL,
    unknown_fields_bytes INTEGER NOT NULL CHECK (unknown_fields_bytes >= 0),
    unknown_fields_sha256 CHAR(64),
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    opened_run_id BIGINT NOT NULL REFERENCES ingestion_runs (id),
    closed_run_id BIGINT REFERENCES ingestion_runs (id),
    UNIQUE (service_alert_id, revision_number),
    UNIQUE (service_alert_id, id)
);

CREATE INDEX service_alert_revisions_alert_time_idx
    ON service_alert_revisions (service_alert_id, first_seen_at DESC);

ALTER TABLE service_alerts
    ADD CONSTRAINT service_alerts_current_revision_fk
        FOREIGN KEY (id, current_revision_id)
        REFERENCES service_alert_revisions (service_alert_id, id);

CREATE TABLE alert_revision_active_periods (
    alert_revision_id BIGINT NOT NULL REFERENCES service_alert_revisions (id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    PRIMARY KEY (alert_revision_id, position)
);

CREATE TABLE alert_revision_informed_entities (
    alert_revision_id BIGINT NOT NULL REFERENCES service_alert_revisions (id) ON DELETE CASCADE,
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
    PRIMARY KEY (alert_revision_id, position)
);

CREATE INDEX alert_revision_entities_route_idx
    ON alert_revision_informed_entities (route_id) WHERE route_id IS NOT NULL;

CREATE INDEX alert_revision_entities_stop_idx
    ON alert_revision_informed_entities (stop_id) WHERE stop_id IS NOT NULL;

-- +goose Down
DROP TABLE alert_revision_informed_entities;
DROP TABLE alert_revision_active_periods;
ALTER TABLE service_alerts DROP CONSTRAINT service_alerts_current_revision_fk;
DROP TABLE service_alert_revisions;
DROP TABLE service_alerts;
DROP INDEX ingestion_runs_source_success_idx;
ALTER TABLE service_alert_snapshots DROP COLUMN unknown_fields_sha256;
UPDATE ingestion_runs
SET status = 'failed',
    error_message = COALESCE(error_message, 'Skipped run converted to failed during schema rollback')
WHERE status = 'skipped';
ALTER TABLE ingestion_runs DROP COLUMN skip_reason;
ALTER TABLE ingestion_runs DROP COLUMN alert_reconciliation_applied;
ALTER TABLE ingestion_runs DROP CONSTRAINT ingestion_runs_status_check;
ALTER TABLE ingestion_runs
    ADD CONSTRAINT ingestion_runs_status_check
        CHECK (status IN ('running', 'succeeded', 'failed'));
