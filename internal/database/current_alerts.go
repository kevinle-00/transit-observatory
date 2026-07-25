package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

type CurrentAlertReader struct {
	db *sql.DB
}

type CurrentAlert struct {
	ID                  int64                      `json:"id"`
	SourceURL           string                     `json:"source_url"`
	SourceEntityID      string                     `json:"source_entity_id"`
	RevisionID          int64                      `json:"revision_id"`
	RevisionNumber      int                        `json:"revision_number"`
	Cause               *string                    `json:"cause,omitempty"`
	Effect              *string                    `json:"effect,omitempty"`
	Severity            *string                    `json:"severity,omitempty"`
	Header              []realtime.Translation     `json:"header"`
	Description         []realtime.Translation     `json:"description"`
	URL                 []realtime.Translation     `json:"url"`
	FirstSeenAt         time.Time                  `json:"first_seen_at"`
	LastSeenAt          time.Time                  `json:"last_seen_at"`
	RevisionFirstSeenAt time.Time                  `json:"revision_first_seen_at"`
	RevisionLastSeenAt  time.Time                  `json:"revision_last_seen_at"`
	ActivePeriods       []CurrentAlertActivePeriod `json:"active_periods"`
	Routes              []CurrentAlertRoute        `json:"routes"`
	Stations            []CurrentAlertStation      `json:"stations"`
}

type CurrentAlertActivePeriod struct {
	Position int        `json:"position"`
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
}

type CurrentAlertRoute struct {
	SourceRouteID    string  `json:"source_route_id"`
	StaticRouteID    *string `json:"static_route_id,omitempty"`
	ShortName        *string `json:"short_name,omitempty"`
	LongName         *string `json:"long_name,omitempty"`
	RouteType        *int    `json:"route_type,omitempty"`
	Color            *string `json:"color,omitempty"`
	TextColor        *string `json:"text_color,omitempty"`
	IsReplacementBus *bool   `json:"is_replacement_bus,omitempty"`
	IsMatched        bool    `json:"is_matched"`
}

type CurrentAlertStation struct {
	SourceStopID       string   `json:"source_stop_id"`
	StaticStationID    *string  `json:"static_station_id,omitempty"`
	Name               *string  `json:"name,omitempty"`
	Latitude           *float64 `json:"latitude,omitempty"`
	Longitude          *float64 `json:"longitude,omitempty"`
	WheelchairBoarding *int     `json:"wheelchair_boarding,omitempty"`
	IsMatched          bool     `json:"is_matched"`
}

func NewCurrentAlertReader(db *sql.DB) *CurrentAlertReader {
	return &CurrentAlertReader{db: db}
}

func (r *CurrentAlertReader) List(ctx context.Context) ([]CurrentAlert, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin current-alert query: %w", err)
	}
	defer tx.Rollback()

	alerts, indexes, err := queryCurrentAlerts(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := queryCurrentAlertPeriods(ctx, tx, alerts, indexes); err != nil {
		return nil, err
	}
	if err := queryCurrentAlertRoutes(ctx, tx, alerts, indexes); err != nil {
		return nil, err
	}
	if err := queryCurrentAlertStations(ctx, tx, alerts, indexes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit current-alert query: %w", err)
	}
	return alerts, nil
}

func queryCurrentAlerts(ctx context.Context, tx *sql.Tx) ([]CurrentAlert, map[int64]int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT alert_id, source_url, source_entity_id, revision_id, revision_number,
			cause, effect, severity, header, description, url,
			alert_first_seen_at, alert_last_seen_at,
			revision_first_seen_at, revision_last_seen_at
		FROM current_alerts
		ORDER BY alert_last_seen_at DESC, source_entity_id
	`)
	if err != nil {
		return nil, nil, fmt.Errorf("query current alerts: %w", err)
	}
	defer rows.Close()
	alerts := []CurrentAlert{}
	indexes := make(map[int64]int)
	for rows.Next() {
		var alert CurrentAlert
		var cause, effect, severity sql.NullString
		var header, description, url []byte
		if err := rows.Scan(
			&alert.ID, &alert.SourceURL, &alert.SourceEntityID, &alert.RevisionID,
			&alert.RevisionNumber, &cause, &effect, &severity, &header, &description,
			&url, &alert.FirstSeenAt, &alert.LastSeenAt, &alert.RevisionFirstSeenAt,
			&alert.RevisionLastSeenAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan current alert: %w", err)
		}
		alert.Cause = nullStringPointer(cause)
		alert.Effect = nullStringPointer(effect)
		alert.Severity = nullStringPointer(severity)
		if err := unmarshalTranslations(header, &alert.Header); err != nil {
			return nil, nil, fmt.Errorf("decode header for alert %q: %w", alert.SourceEntityID, err)
		}
		if err := unmarshalTranslations(description, &alert.Description); err != nil {
			return nil, nil, fmt.Errorf("decode description for alert %q: %w", alert.SourceEntityID, err)
		}
		if err := unmarshalTranslations(url, &alert.URL); err != nil {
			return nil, nil, fmt.Errorf("decode URL for alert %q: %w", alert.SourceEntityID, err)
		}
		alert.ActivePeriods = []CurrentAlertActivePeriod{}
		alert.Routes = []CurrentAlertRoute{}
		alert.Stations = []CurrentAlertStation{}
		indexes[alert.ID] = len(alerts)
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate current alerts: %w", err)
	}
	return alerts, indexes, nil
}

func queryCurrentAlertPeriods(ctx context.Context, tx *sql.Tx, alerts []CurrentAlert, indexes map[int64]int) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT alert_id, position, starts_at, ends_at
		FROM current_alert_active_periods
		ORDER BY alert_id, position
	`)
	if err != nil {
		return fmt.Errorf("query current alert active periods: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var alertID int64
		var period CurrentAlertActivePeriod
		var startsAt, endsAt sql.NullTime
		if err := rows.Scan(&alertID, &period.Position, &startsAt, &endsAt); err != nil {
			return fmt.Errorf("scan current alert active period: %w", err)
		}
		index, exists := indexes[alertID]
		if !exists {
			return fmt.Errorf("active period references unknown current alert %d", alertID)
		}
		period.StartsAt = nullTimePointer(startsAt)
		period.EndsAt = nullTimePointer(endsAt)
		alerts[index].ActivePeriods = append(alerts[index].ActivePeriods, period)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate current alert active periods: %w", err)
	}
	return nil
}

func queryCurrentAlertRoutes(ctx context.Context, tx *sql.Tx, alerts []CurrentAlert, indexes map[int64]int) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT alert_id, source_route_id, static_route_id, short_name, long_name,
			route_type, color, text_color, is_replacement_bus, is_matched
		FROM current_alert_routes
		ORDER BY alert_id, source_route_id
	`)
	if err != nil {
		return fmt.Errorf("query current alert routes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var alertID int64
		var route CurrentAlertRoute
		var staticID, shortName, longName, color, textColor sql.NullString
		var routeType sql.NullInt64
		var replacement sql.NullBool
		if err := rows.Scan(&alertID, &route.SourceRouteID, &staticID, &shortName,
			&longName, &routeType, &color, &textColor, &replacement, &route.IsMatched); err != nil {
			return fmt.Errorf("scan current alert route: %w", err)
		}
		index, exists := indexes[alertID]
		if !exists {
			return fmt.Errorf("route references unknown current alert %d", alertID)
		}
		route.StaticRouteID = nullStringPointer(staticID)
		route.ShortName = nullStringPointer(shortName)
		route.LongName = nullStringPointer(longName)
		route.Color = nullStringPointer(color)
		route.TextColor = nullStringPointer(textColor)
		route.RouteType = nullIntPointer(routeType)
		route.IsReplacementBus = nullBoolPointer(replacement)
		alerts[index].Routes = append(alerts[index].Routes, route)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate current alert routes: %w", err)
	}
	return nil
}

func queryCurrentAlertStations(ctx context.Context, tx *sql.Tx, alerts []CurrentAlert, indexes map[int64]int) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT alert_id, source_stop_id, static_station_id, station_name,
			latitude, longitude, wheelchair_boarding, is_matched
		FROM current_alert_stations
		ORDER BY alert_id, source_stop_id
	`)
	if err != nil {
		return fmt.Errorf("query current alert stations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var alertID int64
		var station CurrentAlertStation
		var staticID, name sql.NullString
		var latitude, longitude sql.NullFloat64
		var wheelchair sql.NullInt64
		if err := rows.Scan(&alertID, &station.SourceStopID, &staticID, &name,
			&latitude, &longitude, &wheelchair, &station.IsMatched); err != nil {
			return fmt.Errorf("scan current alert station: %w", err)
		}
		index, exists := indexes[alertID]
		if !exists {
			return fmt.Errorf("station references unknown current alert %d", alertID)
		}
		station.StaticStationID = nullStringPointer(staticID)
		station.Name = nullStringPointer(name)
		station.Latitude = nullFloatPointer(latitude)
		station.Longitude = nullFloatPointer(longitude)
		station.WheelchairBoarding = nullIntPointer(wheelchair)
		alerts[index].Stations = append(alerts[index].Stations, station)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate current alert stations: %w", err)
	}
	return nil
}

func unmarshalTranslations(value []byte, destination *[]realtime.Translation) error {
	if err := json.Unmarshal(value, destination); err != nil {
		return err
	}
	if *destination == nil {
		*destination = []realtime.Translation{}
	}
	return nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func nullIntPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func nullFloatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullBoolPointer(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}
