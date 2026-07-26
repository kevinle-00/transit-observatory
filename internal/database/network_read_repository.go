package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (r *ReadRepository) ListLines(ctx context.Context, includeReplacementBus bool, now time.Time) ([]LineSummary, error) {
	tx, err := r.readTx(ctx, "line list query")
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lines, err := queryLines(ctx, tx, includeReplacementBus, now.UTC(), "")
	if err != nil {
		return nil, err
	}
	if err := commitReadTx(tx, "line list query"); err != nil {
		return nil, err
	}
	return lines, nil
}

func queryLines(ctx context.Context, tx *sql.Tx, includeReplacementBus bool, now time.Time, lineID string) ([]LineSummary, error) {
	conditions := []string{"($2 OR route.is_replacement_bus = false)"}
	args := []any{now, includeReplacementBus}
	if lineID != "" {
		conditions = append(conditions, "route.route_id = $3")
		args = append(args, lineID)
	}
	rows, err := tx.QueryContext(ctx, `
		WITH current_impacts AS (
			SELECT impact.route_id, alert.id AS alert_id, revision.id AS revision_id
			FROM service_alerts alert
			JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
			JOIN alert_revision_lines impact ON impact.alert_revision_id = revision.id
			WHERE alert.is_present AND NOT revision.is_deleted
		)
		SELECT route.route_id, route.short_name, route.long_name, route.route_type,
			route.color, route.text_color, route.is_replacement_bus,
			count(DISTINCT relation.station_id), `+statusCountsSQL("$1")+`
		FROM routes route
		LEFT JOIN route_stations relation ON relation.route_id = route.route_id
		LEFT JOIN current_impacts impact ON impact.route_id = route.route_id
		LEFT JOIN service_alerts alert ON alert.id = impact.alert_id
		LEFT JOIN service_alert_revisions revision ON revision.id = impact.revision_id
		WHERE `+strings.Join(conditions, " AND ")+`
		GROUP BY route.route_id
		ORDER BY route.short_name, route.long_name, route.route_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("query lines: %w", err)
	}
	defer rows.Close()
	lines := []LineSummary{}
	for rows.Next() {
		line, err := scanLine(rows)
		if err != nil {
			return nil, fmt.Errorf("scan line: %w", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lines: %w", err)
	}
	return lines, nil
}

func (r *ReadRepository) GetLine(ctx context.Context, lineID string, now time.Time) (LineDetail, error) {
	tx, err := r.readTx(ctx, "line detail query")
	if err != nil {
		return LineDetail{}, err
	}
	defer tx.Rollback()
	lines, err := queryLines(ctx, tx, true, now.UTC(), lineID)
	if err != nil {
		return LineDetail{}, err
	}
	if len(lines) == 0 {
		return LineDetail{}, fmt.Errorf("line %q: %w", lineID, ErrNotFound)
	}
	stations, err := queryStations(ctx, tx, StationQuery{LineID: lineID}, now.UTC(), "")
	if err != nil {
		return LineDetail{}, err
	}
	alerts, err := queryImpactAlerts(ctx, tx, "line", lineID)
	if err != nil {
		return LineDetail{}, err
	}
	if err := commitReadTx(tx, "line detail query"); err != nil {
		return LineDetail{}, err
	}
	return LineDetail{Line: lines[0], Stations: stations, Alerts: alerts}, nil
}

func (r *ReadRepository) ListStations(ctx context.Context, query StationQuery, now time.Time) ([]StationSummary, error) {
	tx, err := r.readTx(ctx, "station list query")
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stations, err := queryStations(ctx, tx, query, now.UTC(), "")
	if err != nil {
		return nil, err
	}
	if err := commitReadTx(tx, "station list query"); err != nil {
		return nil, err
	}
	return stations, nil
}

func queryStations(ctx context.Context, tx *sql.Tx, query StationQuery, now time.Time, stationID string) ([]StationSummary, error) {
	conditions := []string{"station.location_type = 1"}
	args := []any{now}
	if query.Q != "" {
		args = append(args, "%"+escapeLike(query.Q)+"%")
		conditions = append(conditions, fmt.Sprintf("station.name ILIKE $%d ESCAPE E'\\\\'", len(args)))
	}
	if query.LineID != "" {
		args = append(args, query.LineID)
		conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM route_stations filter_relation WHERE filter_relation.station_id = station.stop_id AND filter_relation.route_id = $%d)", len(args)))
	}
	if stationID != "" {
		args = append(args, stationID)
		conditions = append(conditions, fmt.Sprintf("station.stop_id = $%d", len(args)))
	}
	rows, err := tx.QueryContext(ctx, `
		WITH current_impacts AS (
			SELECT impact.station_id, alert.id AS alert_id, revision.id AS revision_id
			FROM service_alerts alert
			JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
			JOIN alert_revision_impacted_stations impact ON impact.alert_revision_id = revision.id
			WHERE alert.is_present AND NOT revision.is_deleted
		)
		SELECT station.stop_id, station.name, station.latitude, station.longitude,
			station.wheelchair_boarding, `+statusCountsSQL("$1")+`
		FROM stops station
		LEFT JOIN current_impacts impact ON impact.station_id = station.stop_id
		LEFT JOIN service_alerts alert ON alert.id = impact.alert_id
		LEFT JOIN service_alert_revisions revision ON revision.id = impact.revision_id
		WHERE `+strings.Join(conditions, " AND ")+`
		GROUP BY station.stop_id
		ORDER BY station.name, station.stop_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("query stations: %w", err)
	}
	stations := []StationSummary{}
	indexes := map[string]int{}
	for rows.Next() {
		var station StationSummary
		var latitude, longitude sql.NullFloat64
		var wheelchair sql.NullInt64
		if err := rows.Scan(&station.ID, &station.Name, &latitude, &longitude,
			&wheelchair, &station.PresentAlertCount, &station.CurrentAlertCount,
			&station.UpcomingAlertCount); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan station: %w", err)
		}
		station.Latitude = nullFloatPointer(latitude)
		station.Longitude = nullFloatPointer(longitude)
		station.WheelchairBoarding = nullIntPointer(wheelchair)
		station.Lines = []LineSummary{}
		indexes[station.ID] = len(stations)
		stations = append(stations, station)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close stations: %w", err)
	}
	if len(stations) == 0 {
		return stations, nil
	}
	stationIDs := make([]string, len(stations))
	for index := range stations {
		stationIDs[index] = stations[index].ID
	}
	lineRows, err := tx.QueryContext(ctx, `
		WITH current_impacts AS (
			SELECT impact.route_id, alert.id AS alert_id, revision.id AS revision_id
			FROM service_alerts alert
			JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
			JOIN alert_revision_lines impact ON impact.alert_revision_id = revision.id
			WHERE alert.is_present AND NOT revision.is_deleted
		)
		SELECT relation.station_id, route.route_id, route.short_name, route.long_name,
			route.route_type, route.color, route.text_color, route.is_replacement_bus,
			(SELECT count(*) FROM route_stations count_relation WHERE count_relation.route_id = route.route_id),
			`+statusCountsSQL("$1")+`
		FROM route_stations relation
		JOIN routes route ON route.route_id = relation.route_id
		LEFT JOIN current_impacts impact ON impact.route_id = route.route_id
		LEFT JOIN service_alerts alert ON alert.id = impact.alert_id
		LEFT JOIN service_alert_revisions revision ON revision.id = impact.revision_id
		WHERE relation.station_id = ANY($2)
		GROUP BY relation.station_id, route.route_id
		ORDER BY relation.station_id, route.short_name, route.route_id`, now, stationIDs)
	if err != nil {
		return nil, fmt.Errorf("query station lines: %w", err)
	}
	defer lineRows.Close()
	for lineRows.Next() {
		var stationID string
		var line LineSummary
		var color, textColor sql.NullString
		if err := lineRows.Scan(&stationID, &line.ID, &line.ShortName, &line.LongName,
			&line.RouteType, &color, &textColor, &line.IsReplacementBus,
			&line.StationCount, &line.PresentAlertCount, &line.CurrentAlertCount,
			&line.UpcomingAlertCount); err != nil {
			return nil, fmt.Errorf("scan station line: %w", err)
		}
		line.Color = nullStringPointer(color)
		line.TextColor = nullStringPointer(textColor)
		stations[indexes[stationID]].Lines = append(stations[indexes[stationID]].Lines, line)
	}
	if err := lineRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate station lines: %w", err)
	}
	return stations, nil
}

func (r *ReadRepository) GetStation(ctx context.Context, stationID string, now time.Time) (StationDetail, error) {
	tx, err := r.readTx(ctx, "station detail query")
	if err != nil {
		return StationDetail{}, err
	}
	defer tx.Rollback()
	stations, err := queryStations(ctx, tx, StationQuery{}, now.UTC(), stationID)
	if err != nil {
		return StationDetail{}, err
	}
	if len(stations) == 0 {
		return StationDetail{}, fmt.Errorf("station %q: %w", stationID, ErrNotFound)
	}
	alerts, err := queryImpactAlerts(ctx, tx, "station", stationID)
	if err != nil {
		return StationDetail{}, err
	}
	if err := commitReadTx(tx, "station detail query"); err != nil {
		return StationDetail{}, err
	}
	return StationDetail{Station: stations[0], Alerts: alerts}, nil
}

func queryImpactAlerts(ctx context.Context, tx *sql.Tx, dimension, id string) ([]CurrentAlert, error) {
	view, column := "alert_revision_lines", "route_id"
	if dimension == "station" {
		view, column = "alert_revision_impacted_stations", "station_id"
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT alert.id, alert.source_url, alert.source_entity_id, revision.id,
			revision.revision_number, revision.cause, revision.effect, revision.severity,
			revision.header, revision.description, revision.url, alert.first_seen_at,
			alert.last_seen_at, revision.first_seen_at, revision.last_seen_at, count(*) OVER ()
		FROM service_alerts alert
		JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
		WHERE alert.is_present AND NOT revision.is_deleted
			AND EXISTS (SELECT 1 FROM `+view+` impact WHERE impact.alert_revision_id = revision.id AND impact.`+column+` = $1)
		ORDER BY alert.last_seen_at DESC, alert.source_entity_id, alert.id`, id)
	if err != nil {
		return nil, fmt.Errorf("query %s alerts: %w", dimension, err)
	}
	alerts, _, err := scanAlerts(rows)
	if err != nil {
		return nil, err
	}
	if err := enrichAlerts(ctx, tx, alerts); err != nil {
		return nil, err
	}
	return alerts, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
