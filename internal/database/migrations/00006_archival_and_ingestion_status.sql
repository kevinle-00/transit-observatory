-- +goose Up
CREATE TABLE raw_archives (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    backend TEXT NOT NULL CHECK (backend <> ''),
    object_key TEXT NOT NULL CHECK (object_key <> ''),
    content_sha256 CHAR(64) NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    bytes BIGINT NOT NULL CHECK (bytes >= 0),
    stored_at TIMESTAMPTZ NOT NULL,
    etag TEXT,
    version_id TEXT,
    UNIQUE (backend, object_key)
);

ALTER TABLE ingestion_runs
    ADD COLUMN archive_status TEXT NOT NULL DEFAULT 'legacy'
        CHECK (archive_status IN ('legacy', 'pending', 'archived', 'failed')),
    ADD COLUMN raw_archive_id BIGINT REFERENCES raw_archives (id),
    ADD COLUMN archive_created BOOLEAN,
    ADD COLUMN archive_error TEXT,
    ADD COLUMN skip_code TEXT CHECK (skip_code IN ('duplicate', 'stale')),
    ADD COLUMN failure_stage TEXT CHECK (failure_stage IN ('fetch', 'archive', 'decode', 'parse', 'persist', 'commit', 'cleanup')),
    ADD COLUMN failure_code TEXT,
    ADD COLUMN public_error_message TEXT;

UPDATE ingestion_runs SET skip_code = CASE skip_reason
    WHEN 'byte-identical to the latest applied payload' THEN 'duplicate'
    WHEN 'feed observation is older than the latest applied run' THEN 'stale'
END;

ALTER TABLE ingestion_runs ALTER COLUMN archive_status SET DEFAULT 'pending';
ALTER TABLE ingestion_runs
    ADD CONSTRAINT ingestion_runs_archive_link_check CHECK (
        (archive_status = 'archived' AND raw_archive_id IS NOT NULL AND archive_error IS NULL)
        OR (archive_status = 'failed' AND raw_archive_id IS NULL AND archive_created IS NULL AND archive_error IS NOT NULL)
        OR (archive_status IN ('legacy', 'pending') AND raw_archive_id IS NULL AND archive_created IS NULL AND archive_error IS NULL)
    ),
    ADD CONSTRAINT ingestion_runs_terminal_archive_check CHECK (
        status NOT IN ('succeeded', 'skipped') OR archive_status IN ('archived', 'legacy')
    ),
    ADD CONSTRAINT ingestion_runs_skip_lifecycle_check CHECK (
        (status = 'skipped' AND skip_code IS NOT NULL) OR (status <> 'skipped' AND skip_code IS NULL)
    );

ALTER TABLE gtfs_imports
    ADD COLUMN archive_status TEXT NOT NULL DEFAULT 'legacy'
        CHECK (archive_status IN ('legacy', 'pending', 'archived', 'failed')),
    ADD COLUMN raw_archive_id BIGINT REFERENCES raw_archives (id),
    ADD COLUMN archive_created BOOLEAN,
    ADD COLUMN archive_error TEXT,
    ADD COLUMN skip_code TEXT CHECK (skip_code IN ('duplicate', 'stale')),
    ADD COLUMN failure_stage TEXT CHECK (failure_stage IN ('fetch', 'archive', 'decode', 'parse', 'persist', 'commit', 'cleanup')),
    ADD COLUMN failure_code TEXT,
    ADD COLUMN public_error_message TEXT,
    ADD COLUMN content_type TEXT;

UPDATE gtfs_imports SET skip_code = CASE skip_reason
    WHEN 'content hash matches the currently installed network' THEN 'duplicate'
    WHEN 'source archive is older than the currently installed network' THEN 'stale'
    WHEN 'archive request is older than the currently installed network' THEN 'stale'
END;

ALTER TABLE gtfs_imports ALTER COLUMN archive_status SET DEFAULT 'pending';
ALTER TABLE gtfs_imports
    ADD CONSTRAINT gtfs_imports_archive_link_check CHECK (
        (archive_status = 'archived' AND raw_archive_id IS NOT NULL AND archive_error IS NULL)
        OR (archive_status = 'failed' AND raw_archive_id IS NULL AND archive_created IS NULL AND archive_error IS NOT NULL)
        OR (archive_status IN ('legacy', 'pending') AND raw_archive_id IS NULL AND archive_created IS NULL AND archive_error IS NULL)
    ),
    ADD CONSTRAINT gtfs_imports_terminal_archive_check CHECK (
        status NOT IN ('succeeded', 'skipped') OR archive_status IN ('archived', 'legacy')
    ),
    ADD CONSTRAINT gtfs_imports_skip_lifecycle_check CHECK (
        (status = 'skipped' AND skip_code IS NOT NULL) OR (status <> 'skipped' AND skip_code IS NULL)
    );

CREATE INDEX ingestion_runs_status_latest_idx ON ingestion_runs (started_at DESC, id DESC);
CREATE INDEX ingestion_runs_status_applied_idx
    ON ingestion_runs (COALESCE(feed_timestamp, retrieved_at) DESC, retrieved_at DESC, id DESC)
    WHERE status = 'succeeded' AND alert_reconciliation_applied;
CREATE INDEX ingestion_runs_status_failure_idx
    ON ingestion_runs (completed_at DESC, id DESC) WHERE status = 'failed';
CREATE INDEX ingestion_runs_status_check_idx
    ON ingestion_runs (completed_at DESC, id DESC)
    WHERE (status = 'succeeded' AND alert_reconciliation_applied)
        OR (status = 'skipped' AND skip_code = 'duplicate');
CREATE INDEX gtfs_imports_status_applied_idx
    ON gtfs_imports (COALESCE(source_modified_at, retrieved_at) DESC, id DESC)
    WHERE status = 'succeeded' AND is_current;
CREATE INDEX gtfs_imports_status_failure_idx
    ON gtfs_imports (completed_at DESC, id DESC) WHERE status = 'failed';
CREATE INDEX gtfs_imports_status_check_idx
    ON gtfs_imports (completed_at DESC, id DESC)
    WHERE status = 'succeeded' OR (status = 'skipped' AND skip_code = 'duplicate');

-- +goose Down
DROP INDEX gtfs_imports_status_check_idx;
DROP INDEX gtfs_imports_status_failure_idx;
DROP INDEX gtfs_imports_status_applied_idx;
DROP INDEX ingestion_runs_status_check_idx;
DROP INDEX ingestion_runs_status_failure_idx;
DROP INDEX ingestion_runs_status_applied_idx;
DROP INDEX ingestion_runs_status_latest_idx;

ALTER TABLE gtfs_imports
    DROP CONSTRAINT gtfs_imports_skip_lifecycle_check,
    DROP CONSTRAINT gtfs_imports_terminal_archive_check,
    DROP CONSTRAINT gtfs_imports_archive_link_check,
    DROP COLUMN content_type,
    DROP COLUMN public_error_message,
    DROP COLUMN failure_code,
    DROP COLUMN failure_stage,
    DROP COLUMN skip_code,
    DROP COLUMN archive_error,
    DROP COLUMN archive_created,
    DROP COLUMN raw_archive_id,
    DROP COLUMN archive_status;

ALTER TABLE ingestion_runs
    DROP CONSTRAINT ingestion_runs_skip_lifecycle_check,
    DROP CONSTRAINT ingestion_runs_terminal_archive_check,
    DROP CONSTRAINT ingestion_runs_archive_link_check,
    DROP COLUMN public_error_message,
    DROP COLUMN failure_code,
    DROP COLUMN failure_stage,
    DROP COLUMN skip_code,
    DROP COLUMN archive_error,
    DROP COLUMN archive_created,
    DROP COLUMN raw_archive_id,
    DROP COLUMN archive_status;

DROP TABLE raw_archives;
