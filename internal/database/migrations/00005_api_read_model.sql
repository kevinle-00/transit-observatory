-- +goose Up
-- Supports bounded history/analytics scans while preserving alert-local ordering.
CREATE INDEX service_alert_revisions_history_idx
    ON service_alert_revisions (first_seen_at, service_alert_id, id);

-- Complements the route_id selector index for feeds that populate only trip.route_id.
CREATE INDEX alert_revision_entities_trip_route_idx
    ON alert_revision_informed_entities (trip_route_id)
    WHERE trip_route_id IS NOT NULL;

CREATE VIEW stop_parent_stations AS
WITH RECURSIVE ancestors AS (
    SELECT
        stop.stop_id AS source_stop_id,
        stop.stop_id,
        stop.parent_station_id,
        stop.location_type,
        ARRAY[stop.stop_id] AS path
    FROM stops stop

    UNION ALL

    SELECT
        ancestor.source_stop_id,
        parent.stop_id,
        parent.parent_station_id,
        parent.location_type,
        ancestor.path || parent.stop_id
    FROM ancestors ancestor
    JOIN stops parent ON parent.stop_id = ancestor.parent_station_id
    WHERE ancestor.location_type <> 1
        AND NOT parent.stop_id = ANY(ancestor.path)
)
SELECT DISTINCT source_stop_id, stop_id AS station_id
FROM ancestors
WHERE location_type = 1;

CREATE VIEW alert_revision_route_identifiers AS
SELECT entity.alert_revision_id, identifier.source_route_id
FROM alert_revision_informed_entities entity
JOIN LATERAL (
    SELECT entity.route_id AS source_route_id WHERE entity.route_id IS NOT NULL
    UNION
    SELECT entity.trip_route_id AS source_route_id WHERE entity.trip_route_id IS NOT NULL
) identifier ON true
GROUP BY entity.alert_revision_id, identifier.source_route_id;

CREATE VIEW alert_revision_routes AS
SELECT
    identifier.alert_revision_id,
    identifier.source_route_id,
    route.route_id AS static_route_id,
    route.short_name,
    route.long_name,
    route.route_type,
    route.color,
    route.text_color,
    route.is_replacement_bus,
    route.route_id IS NOT NULL AS is_matched
FROM alert_revision_route_identifiers identifier
LEFT JOIN routes route ON route.route_id = identifier.source_route_id;

CREATE VIEW alert_revision_stations AS
SELECT
    selector.alert_revision_id,
    selector.source_stop_id,
    station.stop_id AS static_station_id,
    station.name AS station_name,
    station.latitude,
    station.longitude,
    station.wheelchair_boarding,
    station.stop_id IS NOT NULL AS is_matched
FROM (
    SELECT DISTINCT alert_revision_id, stop_id AS source_stop_id
    FROM alert_revision_informed_entities
    WHERE stop_id IS NOT NULL
) selector
LEFT JOIN stop_parent_stations parent ON parent.source_stop_id = selector.source_stop_id
LEFT JOIN stops station ON station.stop_id = parent.station_id;

CREATE OR REPLACE VIEW current_alert_routes AS
SELECT
    current.alert_id,
    current.revision_id,
    route.source_route_id,
    route.static_route_id,
    route.short_name,
    route.long_name,
    route.route_type,
    route.color,
    route.text_color,
    route.is_replacement_bus,
    route.is_matched
FROM current_alerts current
JOIN alert_revision_routes route ON route.alert_revision_id = current.revision_id;

CREATE OR REPLACE VIEW current_alert_stations AS
SELECT
    current.alert_id,
    current.revision_id,
    station.source_stop_id,
    station.static_station_id,
    station.station_name,
    station.latitude,
    station.longitude,
    station.wheelchair_boarding,
    station.is_matched
FROM current_alerts current
JOIN alert_revision_stations station ON station.alert_revision_id = current.revision_id;

CREATE VIEW alert_revision_lines AS
SELECT identifier.alert_revision_id, route.route_id
FROM alert_revision_route_identifiers identifier
JOIN routes route ON route.route_id = identifier.source_route_id

UNION

SELECT station.alert_revision_id, relation.route_id
FROM alert_revision_stations station
JOIN route_stations relation ON relation.station_id = station.static_station_id
WHERE station.is_matched;

CREATE VIEW alert_revision_impacted_stations AS
SELECT station.alert_revision_id, station.static_station_id AS station_id
FROM alert_revision_stations station
WHERE station.is_matched

UNION

SELECT identifier.alert_revision_id, relation.station_id
FROM alert_revision_route_identifiers identifier
JOIN routes route ON route.route_id = identifier.source_route_id
JOIN route_stations relation ON relation.route_id = route.route_id;

CREATE VIEW alert_revision_episode_membership AS
WITH ordered AS (
    SELECT
        revision.*,
        lag(revision.id) OVER (
            PARTITION BY revision.service_alert_id
            ORDER BY revision.first_seen_at, revision.revision_number, revision.id
        ) AS previous_revision_id,
        lag(revision.closed_at) OVER (
            PARTITION BY revision.service_alert_id
            ORDER BY revision.first_seen_at, revision.revision_number, revision.id
        ) AS previous_closed_at,
        lag(revision.is_deleted) OVER (
            PARTITION BY revision.service_alert_id
            ORDER BY revision.first_seen_at, revision.revision_number, revision.id
        ) AS previous_is_deleted
    FROM service_alert_revisions revision
), numbered AS (
    SELECT
        ordered.*,
        sum(CASE
            WHEN previous_revision_id IS NULL
                OR (previous_is_deleted AND NOT is_deleted)
                OR previous_closed_at IS DISTINCT FROM first_seen_at
            THEN 1
            ELSE 0
        END) OVER (
            PARTITION BY service_alert_id
            ORDER BY first_seen_at, revision_number, id
        ) AS episode_number
    FROM ordered
)
SELECT
    id AS alert_revision_id,
    service_alert_id,
    episode_number,
    first_seen_at,
    last_seen_at,
    closed_at,
    is_deleted
FROM numbered;

CREATE VIEW alert_episodes AS
SELECT
    membership.service_alert_id,
    membership.episode_number,
    min(membership.first_seen_at) AS first_seen_at,
    CASE
        WHEN bool_or(membership.is_deleted)
            THEN min(membership.first_seen_at) FILTER (WHERE membership.is_deleted)
        ELSE max(membership.last_seen_at)
    END AS last_seen_at,
    CASE
        WHEN bool_or(membership.is_deleted)
            THEN min(membership.first_seen_at) FILTER (WHERE membership.is_deleted)
        ELSE max(membership.closed_at) FILTER (
            WHERE membership.alert_revision_id = final_revision.alert_revision_id
        )
    END AS closed_at
FROM alert_revision_episode_membership membership
JOIN LATERAL (
    SELECT candidate.alert_revision_id
    FROM alert_revision_episode_membership candidate
    WHERE candidate.service_alert_id = membership.service_alert_id
        AND candidate.episode_number = membership.episode_number
    ORDER BY candidate.first_seen_at DESC, candidate.alert_revision_id DESC
    LIMIT 1
) final_revision ON true
GROUP BY membership.service_alert_id, membership.episode_number;

-- +goose Down
DROP VIEW alert_episodes;
DROP VIEW alert_revision_episode_membership;
DROP VIEW alert_revision_impacted_stations;
DROP VIEW alert_revision_lines;
DROP VIEW current_alert_stations;
DROP VIEW current_alert_routes;
DROP VIEW alert_revision_stations;
DROP VIEW alert_revision_routes;
DROP VIEW alert_revision_route_identifiers;
DROP VIEW stop_parent_stations;

CREATE VIEW current_alert_routes AS
SELECT DISTINCT
    current.alert_id, current.revision_id, identifier.source_route_id,
    route.route_id AS static_route_id, route.short_name, route.long_name,
    route.route_type, route.color, route.text_color, route.is_replacement_bus,
    route.route_id IS NOT NULL AS is_matched
FROM current_alerts current
JOIN alert_revision_informed_entities entity ON entity.alert_revision_id = current.revision_id
JOIN LATERAL (
    SELECT entity.route_id AS source_route_id WHERE entity.route_id IS NOT NULL
    UNION
    SELECT entity.trip_route_id AS source_route_id WHERE entity.trip_route_id IS NOT NULL
) identifier ON true
LEFT JOIN routes route ON route.route_id = identifier.source_route_id;

CREATE VIEW current_alert_stations AS
WITH RECURSIVE current_stops AS (
    SELECT DISTINCT current.alert_id, current.revision_id, entity.stop_id AS source_stop_id
    FROM current_alerts current
    JOIN alert_revision_informed_entities entity ON entity.alert_revision_id = current.revision_id
    WHERE entity.stop_id IS NOT NULL
), stop_ancestors AS (
    SELECT identifier.source_stop_id, stop.stop_id, stop.parent_station_id, stop.location_type
    FROM (SELECT DISTINCT source_stop_id FROM current_stops) identifier
    JOIN stops stop ON stop.stop_id = identifier.source_stop_id
    UNION ALL
    SELECT ancestor.source_stop_id, parent.stop_id, parent.parent_station_id, parent.location_type
    FROM stop_ancestors ancestor
    JOIN stops parent ON parent.stop_id = ancestor.parent_station_id
    WHERE ancestor.location_type <> 1
)
SELECT DISTINCT current.alert_id, current.revision_id, current.source_stop_id,
    station.stop_id AS static_station_id, station.name AS station_name,
    station.latitude, station.longitude, station.wheelchair_boarding,
    station.stop_id IS NOT NULL AS is_matched
FROM current_stops current
LEFT JOIN stop_ancestors ancestor
    ON ancestor.source_stop_id = current.source_stop_id AND ancestor.location_type = 1
LEFT JOIN stops station ON station.stop_id = ancestor.stop_id;

DROP INDEX alert_revision_entities_trip_route_idx;
DROP INDEX service_alert_revisions_history_idx;
