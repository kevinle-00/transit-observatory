-- +goose Up
CREATE TABLE gtfs_imports (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'skipped')),
    source_url TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    requested_at TIMESTAMPTZ,
    retrieved_at TIMESTAMPTZ,
    source_modified_at TIMESTAMPTZ,
    content_sha256 CHAR(64),
    archive_bytes BIGINT,
    metro_archive_bytes BIGINT,
    etag TEXT,
    last_modified TEXT,
    route_count INTEGER,
    stop_count INTEGER,
    station_count INTEGER,
    trip_count INTEGER,
    stop_time_count INTEGER,
    route_station_count INTEGER,
    skipped_stop_time_count INTEGER,
    is_current BOOLEAN NOT NULL DEFAULT false,
    skip_reason TEXT,
    error_message TEXT,
    CHECK (NOT is_current OR status = 'succeeded')
);

CREATE INDEX gtfs_imports_successful_hash_idx
    ON gtfs_imports (content_sha256) WHERE status = 'succeeded';

CREATE UNIQUE INDEX gtfs_imports_one_current_idx
    ON gtfs_imports (is_current) WHERE is_current;

CREATE INDEX gtfs_imports_started_idx
    ON gtfs_imports (started_at DESC);

CREATE TABLE routes (
    route_id TEXT PRIMARY KEY,
    gtfs_import_id BIGINT NOT NULL REFERENCES gtfs_imports (id),
    agency_id TEXT,
    short_name TEXT NOT NULL,
    long_name TEXT NOT NULL,
    route_type INTEGER NOT NULL,
    color CHAR(6),
    text_color CHAR(6),
    is_replacement_bus BOOLEAN NOT NULL
);

CREATE INDEX routes_import_idx ON routes (gtfs_import_id);

CREATE TABLE stops (
    stop_id TEXT PRIMARY KEY,
    gtfs_import_id BIGINT NOT NULL REFERENCES gtfs_imports (id),
    name TEXT NOT NULL,
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    url TEXT,
    location_type INTEGER NOT NULL,
    parent_station_id TEXT,
    wheelchair_boarding INTEGER,
    level_id TEXT,
    platform_code TEXT,
    expected_parent_location_type INTEGER GENERATED ALWAYS AS (
        CASE
            WHEN parent_station_id IS NULL THEN NULL
            WHEN location_type = 4 THEN 0
            ELSE 1
        END
    ) STORED,
    CHECK (location_type BETWEEN 0 AND 4),
    CHECK (
        (location_type = 1 AND parent_station_id IS NULL)
        OR (location_type = 0)
        OR (location_type IN (2, 3, 4) AND parent_station_id IS NOT NULL)
    )
);

ALTER TABLE stops
    ADD CONSTRAINT stops_id_location_unique UNIQUE (stop_id, location_type),
    ADD CONSTRAINT stops_parent_station_fk
        FOREIGN KEY (parent_station_id, expected_parent_location_type)
        REFERENCES stops (stop_id, location_type)
        DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX stops_import_idx ON stops (gtfs_import_id);
CREATE INDEX stops_parent_station_idx ON stops (parent_station_id) WHERE parent_station_id IS NOT NULL;
CREATE INDEX stops_name_idx ON stops (name);

CREATE TABLE route_stations (
    route_id TEXT NOT NULL REFERENCES routes (route_id) ON DELETE CASCADE,
    station_id TEXT NOT NULL,
    station_location_type INTEGER NOT NULL DEFAULT 1 CHECK (station_location_type = 1),
    FOREIGN KEY (station_id, station_location_type)
        REFERENCES stops (stop_id, location_type) ON DELETE CASCADE,
    PRIMARY KEY (route_id, station_id)
);

CREATE INDEX route_stations_station_idx ON route_stations (station_id);

-- +goose Down
DROP TABLE route_stations;
DROP TABLE stops;
DROP TABLE routes;
DROP TABLE gtfs_imports;
