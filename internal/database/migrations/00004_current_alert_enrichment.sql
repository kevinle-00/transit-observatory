-- +goose Up
CREATE VIEW current_alerts AS
SELECT
    alert.id AS alert_id,
    alert.source_url,
    alert.source_entity_id,
    alert.first_seen_at AS alert_first_seen_at,
    alert.last_seen_at AS alert_last_seen_at,
    revision.id AS revision_id,
    revision.revision_number,
    revision.cause,
    revision.effect,
    revision.severity,
    revision.header,
    revision.description,
    revision.url,
    revision.first_seen_at AS revision_first_seen_at,
    revision.last_seen_at AS revision_last_seen_at
FROM service_alerts alert
JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
WHERE alert.is_present AND NOT revision.is_deleted;

CREATE VIEW current_alert_active_periods AS
SELECT
    current.alert_id,
    current.revision_id,
    period.position,
    period.starts_at,
    period.ends_at
FROM current_alerts current
JOIN alert_revision_active_periods period ON period.alert_revision_id = current.revision_id;

CREATE VIEW current_alert_routes AS
SELECT DISTINCT
    current.alert_id,
    current.revision_id,
    identifier.source_route_id,
    route.route_id AS static_route_id,
    route.short_name,
    route.long_name,
    route.route_type,
    route.color,
    route.text_color,
    route.is_replacement_bus,
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
    SELECT DISTINCT
        current.alert_id,
        current.revision_id,
        entity.stop_id AS source_stop_id
    FROM current_alerts current
    JOIN alert_revision_informed_entities entity ON entity.alert_revision_id = current.revision_id
    WHERE entity.stop_id IS NOT NULL
),
stop_ancestors AS (
    SELECT
        identifier.source_stop_id,
        stop.stop_id,
        stop.parent_station_id,
        stop.location_type
    FROM (SELECT DISTINCT source_stop_id FROM current_stops) identifier
    JOIN stops stop ON stop.stop_id = identifier.source_stop_id

    UNION ALL

    SELECT
        ancestor.source_stop_id,
        parent.stop_id,
        parent.parent_station_id,
        parent.location_type
    FROM stop_ancestors ancestor
    JOIN stops parent ON parent.stop_id = ancestor.parent_station_id
    WHERE ancestor.location_type <> 1
)
SELECT DISTINCT
    current.alert_id,
    current.revision_id,
    current.source_stop_id,
    station.stop_id AS static_station_id,
    station.name AS station_name,
    station.latitude,
    station.longitude,
    station.wheelchair_boarding,
    station.stop_id IS NOT NULL AS is_matched
FROM current_stops current
LEFT JOIN stop_ancestors ancestor
    ON ancestor.source_stop_id = current.source_stop_id AND ancestor.location_type = 1
LEFT JOIN stops station ON station.stop_id = ancestor.stop_id;

-- +goose Down
DROP VIEW current_alert_stations;
DROP VIEW current_alert_routes;
DROP VIEW current_alert_active_periods;
DROP VIEW current_alerts;
