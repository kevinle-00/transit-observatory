package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type ReadRepository struct {
	db *sql.DB
}

func NewReadRepository(db *sql.DB) *ReadRepository {
	return &ReadRepository{db: db}
}

func (r *ReadRepository) readTx(ctx context.Context, operation string) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin %s: %w", operation, err)
	}
	return tx, nil
}

func commitReadTx(tx *sql.Tx, operation string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

func (r *ReadRepository) ListAlerts(ctx context.Context, query AlertQuery) (AlertPage, error) {
	if query.Status == AlertStatusHistorical {
		return r.listHistoricalAlerts(ctx, query)
	}
	tx, err := r.readTx(ctx, "alert list query")
	if err != nil {
		return AlertPage{}, err
	}
	defer tx.Rollback()

	conditions := []string{"$1::timestamptz IS NOT NULL"}
	args := []any{query.Now.UTC()}
	statusCurrent := `(NOT EXISTS (
		SELECT 1 FROM alert_revision_active_periods period
		WHERE period.alert_revision_id = revision.id
	) OR EXISTS (
		SELECT 1 FROM alert_revision_active_periods period
		WHERE period.alert_revision_id = revision.id
			AND (period.starts_at IS NULL OR period.starts_at <= $1)
			AND (period.ends_at IS NULL OR period.ends_at > $1)
	))`
	present := `alert.is_present AND NOT revision.is_deleted`
	switch query.Status {
	case AlertStatusPresent:
		conditions = append(conditions, present)
	case AlertStatusCurrent:
		conditions = append(conditions, present, statusCurrent)
	case AlertStatusUpcoming:
		conditions = append(conditions, present, "NOT "+statusCurrent, `EXISTS (
			SELECT 1 FROM alert_revision_active_periods period
			WHERE period.alert_revision_id = revision.id AND period.starts_at > $1
		)`)
	case AlertStatusHistorical:
		conditions = append(conditions, "(NOT alert.is_present OR revision.is_deleted)")
	}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if query.LineID != "" {
		placeholder := addArg(query.LineID)
		conditions = append(conditions, "EXISTS (SELECT 1 FROM alert_revision_lines impact WHERE impact.alert_revision_id = revision.id AND impact.route_id = "+placeholder+")")
	}
	if query.StationID != "" {
		placeholder := addArg(query.StationID)
		conditions = append(conditions, "EXISTS (SELECT 1 FROM alert_revision_impacted_stations impact WHERE impact.alert_revision_id = revision.id AND impact.station_id = "+placeholder+")")
	}
	if query.Cause != "" {
		conditions = append(conditions, "revision.cause = "+addArg(query.Cause))
	}
	if query.Effect != "" {
		conditions = append(conditions, "revision.effect = "+addArg(query.Effect))
	}
	if query.From != nil || query.To != nil {
		periodConditions := []string{"period.alert_revision_id = revision.id"}
		observedConditions := []string{}
		if query.From != nil {
			placeholder := addArg(query.From.UTC())
			periodConditions = append(periodConditions, "(period.ends_at IS NULL OR period.ends_at > "+placeholder+")")
			observedConditions = append(observedConditions, "COALESCE(revision.closed_at, 'infinity'::timestamptz) > "+placeholder)
		}
		if query.To != nil {
			placeholder := addArg(query.To.UTC())
			periodConditions = append(periodConditions, "(period.starts_at IS NULL OR period.starts_at < "+placeholder+")")
			observedConditions = append(observedConditions, "revision.first_seen_at < "+placeholder)
		}
		conditions = append(conditions, `((NOT EXISTS (SELECT 1 FROM alert_revision_active_periods WHERE alert_revision_id = revision.id)
			AND `+strings.Join(observedConditions, " AND ")+`)
			OR EXISTS (SELECT 1 FROM alert_revision_active_periods period WHERE `+strings.Join(periodConditions, " AND ")+`))`)
	}
	where := "TRUE"
	if len(conditions) > 0 {
		where = strings.Join(conditions, " AND ")
	}
	limit := ""
	page, pageSize := 0, 0
	total := 0
	if query.Status == AlertStatusHistorical {
		page, pageSize = query.Page, query.PageSize
		if err := tx.QueryRowContext(ctx, `
			SELECT count(*)
			FROM service_alerts alert
			JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
			WHERE `+where, args...).Scan(&total); err != nil {
			return AlertPage{}, fmt.Errorf("count historical alerts: %w", err)
		}
		offset := int64(page-1) * int64(pageSize)
		limit = " LIMIT " + addArg(pageSize) + " OFFSET " + addArg(offset)
	}
	order := "alert.last_seen_at DESC, alert.source_entity_id, alert.id"
	if query.Status == AlertStatusHistorical {
		order = `CASE
			WHEN NOT alert.is_present THEN alert.closed_at
			WHEN revision.is_deleted THEN revision.first_seen_at
		END DESC NULLS LAST, alert.id DESC`
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT alert.id, alert.source_url, alert.source_entity_id, revision.id,
			revision.revision_number, revision.cause, revision.effect, revision.severity,
			revision.header, revision.description, revision.url, alert.first_seen_at,
			alert.last_seen_at, revision.first_seen_at, revision.last_seen_at,
			count(*) OVER ()
		FROM service_alerts alert
		JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
		WHERE `+where+`
		ORDER BY `+order+limit, args...)
	if err != nil {
		return AlertPage{}, fmt.Errorf("query alerts: %w", err)
	}
	alerts, windowTotal, err := scanAlerts(rows)
	if err != nil {
		return AlertPage{}, err
	}
	if query.Status != AlertStatusHistorical {
		total = windowTotal
	}
	if err := enrichAlerts(ctx, tx, alerts); err != nil {
		return AlertPage{}, err
	}
	if err := commitReadTx(tx, "alert list query"); err != nil {
		return AlertPage{}, err
	}
	return AlertPage{Alerts: alerts, Page: page, PageSize: pageSize, Total: total}, nil
}

func (r *ReadRepository) listHistoricalAlerts(ctx context.Context, query AlertQuery) (AlertPage, error) {
	tx, err := r.readTx(ctx, "historical alert list query")
	if err != nil {
		return AlertPage{}, err
	}
	defer tx.Rollback()
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	conditions := []string{"NOT revision.is_deleted"}
	if query.LineID != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM alert_revision_lines impact WHERE impact.alert_revision_id = revision.id AND impact.route_id = "+addArg(query.LineID)+")")
	}
	if query.StationID != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM alert_revision_impacted_stations impact WHERE impact.alert_revision_id = revision.id AND impact.station_id = "+addArg(query.StationID)+")")
	}
	if query.Cause != "" {
		conditions = append(conditions, "revision.cause = "+addArg(query.Cause))
	}
	if query.Effect != "" {
		conditions = append(conditions, "revision.effect = "+addArg(query.Effect))
	}
	if query.From != nil || query.To != nil {
		periodConditions := []string{"period.alert_revision_id = revision.id"}
		observedConditions := []string{}
		if query.From != nil {
			placeholder := addArg(query.From.UTC())
			periodConditions = append(periodConditions, "(period.ends_at IS NULL OR period.ends_at > "+placeholder+")")
			observedConditions = append(observedConditions, "COALESCE(revision.closed_at, 'infinity'::timestamptz) > "+placeholder)
		}
		if query.To != nil {
			placeholder := addArg(query.To.UTC())
			periodConditions = append(periodConditions, "(period.starts_at IS NULL OR period.starts_at < "+placeholder+")")
			observedConditions = append(observedConditions, "revision.first_seen_at < "+placeholder)
		}
		conditions = append(conditions, `((NOT EXISTS (SELECT 1 FROM alert_revision_active_periods WHERE alert_revision_id = revision.id)
			AND `+strings.Join(observedConditions, " AND ")+`)
			OR EXISTS (SELECT 1 FROM alert_revision_active_periods period WHERE `+strings.Join(periodConditions, " AND ")+`))`)
	}
	from := `
		FROM service_alerts alert
		JOIN service_alert_revisions terminal ON terminal.id = alert.current_revision_id
		JOIN LATERAL (
			SELECT revision.*
			FROM service_alert_revisions revision
			WHERE revision.service_alert_id = alert.id AND ` + strings.Join(conditions, " AND ") + `
			ORDER BY revision.first_seen_at DESC, revision.revision_number DESC, revision.id DESC
			LIMIT 1
		) revision ON true
		WHERE NOT alert.is_present OR terminal.is_deleted`
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) "+from, args...).Scan(&total); err != nil {
		return AlertPage{}, fmt.Errorf("count historical alerts: %w", err)
	}
	offset := int64(query.Page-1) * int64(query.PageSize)
	args = append(args, query.PageSize, offset)
	rows, err := tx.QueryContext(ctx, `
		SELECT alert.id, alert.source_url, alert.source_entity_id, revision.id,
			revision.revision_number, revision.cause, revision.effect, revision.severity,
			revision.header, revision.description, revision.url, alert.first_seen_at,
			alert.last_seen_at, revision.first_seen_at, revision.last_seen_at,
			count(*) OVER ()
		`+from+`
		ORDER BY CASE
			WHEN NOT alert.is_present THEN alert.closed_at
			ELSE terminal.first_seen_at
		END DESC NULLS LAST, alert.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return AlertPage{}, fmt.Errorf("query historical alerts: %w", err)
	}
	alerts, _, err := scanAlerts(rows)
	if err != nil {
		return AlertPage{}, err
	}
	if err := enrichAlerts(ctx, tx, alerts); err != nil {
		return AlertPage{}, err
	}
	if err := commitReadTx(tx, "historical alert list query"); err != nil {
		return AlertPage{}, err
	}
	return AlertPage{Alerts: alerts, Page: query.Page, PageSize: query.PageSize, Total: total}, nil
}

func scanAlerts(rows *sql.Rows) ([]CurrentAlert, int, error) {
	defer rows.Close()
	alerts := []CurrentAlert{}
	total := 0
	for rows.Next() {
		var alert CurrentAlert
		var cause, effect, severity sql.NullString
		var header, description, url []byte
		if err := rows.Scan(&alert.ID, &alert.SourceURL, &alert.SourceEntityID,
			&alert.RevisionID, &alert.RevisionNumber, &cause, &effect, &severity,
			&header, &description, &url, &alert.FirstSeenAt, &alert.LastSeenAt,
			&alert.RevisionFirstSeenAt, &alert.RevisionLastSeenAt, &total); err != nil {
			return nil, 0, fmt.Errorf("scan alert: %w", err)
		}
		alert.Cause = nullStringPointer(cause)
		alert.Effect = nullStringPointer(effect)
		alert.Severity = nullStringPointer(severity)
		if err := unmarshalTranslations(header, &alert.Header); err != nil {
			return nil, 0, fmt.Errorf("decode alert header: %w", err)
		}
		if err := unmarshalTranslations(description, &alert.Description); err != nil {
			return nil, 0, fmt.Errorf("decode alert description: %w", err)
		}
		if err := unmarshalTranslations(url, &alert.URL); err != nil {
			return nil, 0, fmt.Errorf("decode alert URL: %w", err)
		}
		alert.ActivePeriods = []CurrentAlertActivePeriod{}
		alert.Routes = []CurrentAlertRoute{}
		alert.Stations = []CurrentAlertStation{}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate alerts: %w", err)
	}
	return alerts, total, nil
}

func enrichAlerts(ctx context.Context, tx *sql.Tx, alerts []CurrentAlert) error {
	if len(alerts) == 0 {
		return nil
	}
	indexes := make(map[int64]int, len(alerts))
	revisionIDs := make([]int64, len(alerts))
	for index := range alerts {
		indexes[alerts[index].RevisionID] = index
		revisionIDs[index] = alerts[index].RevisionID
	}
	periodRows, err := tx.QueryContext(ctx, `
		SELECT alert_revision_id, position, starts_at, ends_at
		FROM alert_revision_active_periods
		WHERE alert_revision_id = ANY($1)
		ORDER BY alert_revision_id, position`, revisionIDs)
	if err != nil {
		return fmt.Errorf("query selected alert periods: %w", err)
	}
	for periodRows.Next() {
		var revisionID int64
		var period CurrentAlertActivePeriod
		var startsAt, endsAt sql.NullTime
		if err := periodRows.Scan(&revisionID, &period.Position, &startsAt, &endsAt); err != nil {
			periodRows.Close()
			return fmt.Errorf("scan selected alert period: %w", err)
		}
		period.StartsAt = nullTimePointer(startsAt)
		period.EndsAt = nullTimePointer(endsAt)
		alerts[indexes[revisionID]].ActivePeriods = append(alerts[indexes[revisionID]].ActivePeriods, period)
	}
	if err := periodRows.Close(); err != nil {
		return fmt.Errorf("close selected alert periods: %w", err)
	}
	routeRows, err := tx.QueryContext(ctx, `
		SELECT alert_revision_id, source_route_id, static_route_id, short_name,
			long_name, route_type, color, text_color, is_replacement_bus, is_matched
		FROM alert_revision_routes
		WHERE alert_revision_id = ANY($1)
		ORDER BY alert_revision_id, source_route_id`, revisionIDs)
	if err != nil {
		return fmt.Errorf("query selected alert routes: %w", err)
	}
	for routeRows.Next() {
		var revisionID int64
		var route CurrentAlertRoute
		var staticID, shortName, longName, color, textColor sql.NullString
		var routeType sql.NullInt64
		var replacement sql.NullBool
		if err := routeRows.Scan(&revisionID, &route.SourceRouteID, &staticID,
			&shortName, &longName, &routeType, &color, &textColor, &replacement,
			&route.IsMatched); err != nil {
			routeRows.Close()
			return fmt.Errorf("scan selected alert route: %w", err)
		}
		route.StaticRouteID = nullStringPointer(staticID)
		route.ShortName = nullStringPointer(shortName)
		route.LongName = nullStringPointer(longName)
		route.RouteType = nullIntPointer(routeType)
		route.Color = nullStringPointer(color)
		route.TextColor = nullStringPointer(textColor)
		route.IsReplacementBus = nullBoolPointer(replacement)
		alerts[indexes[revisionID]].Routes = append(alerts[indexes[revisionID]].Routes, route)
	}
	if err := routeRows.Close(); err != nil {
		return fmt.Errorf("close selected alert routes: %w", err)
	}
	stationRows, err := tx.QueryContext(ctx, `
		SELECT alert_revision_id, source_stop_id, static_station_id, station_name,
			latitude, longitude, wheelchair_boarding, is_matched
		FROM alert_revision_stations
		WHERE alert_revision_id = ANY($1)
		ORDER BY alert_revision_id, source_stop_id`, revisionIDs)
	if err != nil {
		return fmt.Errorf("query selected alert stations: %w", err)
	}
	defer stationRows.Close()
	for stationRows.Next() {
		var revisionID int64
		var station CurrentAlertStation
		var staticID, name sql.NullString
		var latitude, longitude sql.NullFloat64
		var wheelchair sql.NullInt64
		if err := stationRows.Scan(&revisionID, &station.SourceStopID, &staticID,
			&name, &latitude, &longitude, &wheelchair, &station.IsMatched); err != nil {
			return fmt.Errorf("scan selected alert station: %w", err)
		}
		station.StaticStationID = nullStringPointer(staticID)
		station.Name = nullStringPointer(name)
		station.Latitude = nullFloatPointer(latitude)
		station.Longitude = nullFloatPointer(longitude)
		station.WheelchairBoarding = nullIntPointer(wheelchair)
		alerts[indexes[revisionID]].Stations = append(alerts[indexes[revisionID]].Stations, station)
	}
	if err := stationRows.Err(); err != nil {
		return fmt.Errorf("iterate selected alert stations: %w", err)
	}
	return nil
}

func (r *ReadRepository) GetAlert(ctx context.Context, id int64) (AlertDetail, error) {
	tx, err := r.readTx(ctx, "alert detail query")
	if err != nil {
		return AlertDetail{}, err
	}
	defer tx.Rollback()
	var detail AlertDetail
	var closedAt sql.NullTime
	var isPresent, isDeleted bool
	err = tx.QueryRowContext(ctx, `
		SELECT alert.id, alert.source_url, alert.source_entity_id, alert.first_seen_at,
			alert.last_seen_at,
			CASE WHEN revision.is_deleted THEN revision.first_seen_at ELSE alert.closed_at END,
			alert.is_present, revision.is_deleted,
			count(history.id)
		FROM service_alerts alert
		JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
		JOIN service_alert_revisions history ON history.service_alert_id = alert.id
		WHERE alert.id = $1
		GROUP BY alert.id, revision.is_deleted, revision.first_seen_at`, id).Scan(&detail.ID, &detail.SourceURL,
		&detail.SourceEntityID, &detail.FirstSeenAt, &detail.LastSeenAt, &closedAt,
		&isPresent, &isDeleted, &detail.RevisionCount)
	if err == sql.ErrNoRows {
		return AlertDetail{}, fmt.Errorf("alert %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return AlertDetail{}, fmt.Errorf("query alert %d: %w", id, err)
	}
	detail.ClosedAt = nullTimePointer(closedAt)
	detail.Status = AlertStatusHistorical
	if isPresent && !isDeleted {
		detail.Status = AlertStatusPresent
	}
	revisions, err := loadAlertRevisions(ctx, tx, id, false)
	if err != nil {
		return AlertDetail{}, err
	}
	detail.LatestRevision = revisions[len(revisions)-1]
	if err := commitReadTx(tx, "alert detail query"); err != nil {
		return AlertDetail{}, err
	}
	return detail, nil
}

func (r *ReadRepository) ListAlertRevisions(ctx context.Context, id int64) ([]AlertRevision, error) {
	tx, err := r.readTx(ctx, "alert revision query")
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM service_alerts WHERE id = $1)", id).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check alert %d: %w", id, err)
	}
	if !exists {
		return nil, fmt.Errorf("alert %d: %w", id, ErrNotFound)
	}
	revisions, err := loadAlertRevisions(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if err := commitReadTx(tx, "alert revision query"); err != nil {
		return nil, err
	}
	return revisions, nil
}

func loadAlertRevisions(ctx context.Context, tx *sql.Tx, id int64, all bool) ([]AlertRevision, error) {
	limit := ""
	order := "ASC"
	if !all {
		limit = " LIMIT 1"
		order = "DESC"
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT alert.id, alert.source_url, alert.source_entity_id, revision.id,
			revision.revision_number, revision.cause, revision.effect, revision.severity,
			revision.header, revision.description, revision.url, alert.first_seen_at,
			alert.last_seen_at, revision.first_seen_at, revision.last_seen_at,
			revision.is_deleted, revision.closed_at
		FROM service_alerts alert
		JOIN service_alert_revisions revision ON revision.service_alert_id = alert.id
		WHERE alert.id = $1
		ORDER BY revision.first_seen_at `+order+`, revision.revision_number `+order+`, revision.id `+order+limit, id)
	if err != nil {
		return nil, fmt.Errorf("query revisions for alert %d: %w", id, err)
	}
	defer rows.Close()
	revisions := []AlertRevision{}
	alerts := []CurrentAlert{}
	for rows.Next() {
		var revision AlertRevision
		var cause, effect, severity sql.NullString
		var header, description, rawURL []byte
		var closedAt sql.NullTime
		if err := rows.Scan(&revision.ID, &revision.SourceURL, &revision.SourceEntityID,
			&revision.RevisionID, &revision.RevisionNumber, &cause, &effect, &severity,
			&header, &description, &rawURL, &revision.FirstSeenAt, &revision.LastSeenAt,
			&revision.RevisionFirstSeenAt, &revision.RevisionLastSeenAt,
			&revision.IsDeleted, &closedAt); err != nil {
			return nil, fmt.Errorf("scan alert revision: %w", err)
		}
		revision.Cause = nullStringPointer(cause)
		revision.Effect = nullStringPointer(effect)
		revision.Severity = nullStringPointer(severity)
		revision.ClosedAt = nullTimePointer(closedAt)
		if err := unmarshalTranslations(header, &revision.Header); err != nil {
			return nil, fmt.Errorf("decode revision header: %w", err)
		}
		if err := unmarshalTranslations(description, &revision.Description); err != nil {
			return nil, fmt.Errorf("decode revision description: %w", err)
		}
		if err := unmarshalTranslations(rawURL, &revision.URL); err != nil {
			return nil, fmt.Errorf("decode revision URL: %w", err)
		}
		revision.ActivePeriods = []CurrentAlertActivePeriod{}
		revision.Routes = []CurrentAlertRoute{}
		revision.Stations = []CurrentAlertStation{}
		revisions = append(revisions, revision)
		alerts = append(alerts, revision.CurrentAlert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert revisions: %w", err)
	}
	if err := enrichAlerts(ctx, tx, alerts); err != nil {
		return nil, err
	}
	for index := range revisions {
		revisions[index].CurrentAlert = alerts[index]
	}
	if !all && len(revisions) == 1 {
		return revisions, nil
	}
	return revisions, nil
}

func statusCountsSQL(nowPlaceholder string) string {
	current := `(NOT EXISTS (SELECT 1 FROM alert_revision_active_periods period WHERE period.alert_revision_id = revision.id)
		OR EXISTS (SELECT 1 FROM alert_revision_active_periods period WHERE period.alert_revision_id = revision.id
			AND (period.starts_at IS NULL OR period.starts_at <= ` + nowPlaceholder + `)
			AND (period.ends_at IS NULL OR period.ends_at > ` + nowPlaceholder + `)))`
	return `count(DISTINCT alert.id) FILTER (WHERE alert.is_present AND NOT revision.is_deleted),
		count(DISTINCT alert.id) FILTER (WHERE alert.is_present AND NOT revision.is_deleted AND ` + current + `),
		count(DISTINCT alert.id) FILTER (WHERE alert.is_present AND NOT revision.is_deleted AND NOT ` + current + `
			AND EXISTS (SELECT 1 FROM alert_revision_active_periods period WHERE period.alert_revision_id = revision.id AND period.starts_at > ` + nowPlaceholder + `))`
}

func scanLine(scanner interface{ Scan(...any) error }) (LineSummary, error) {
	var line LineSummary
	var color, textColor sql.NullString
	if err := scanner.Scan(&line.ID, &line.ShortName, &line.LongName, &line.RouteType,
		&color, &textColor, &line.IsReplacementBus, &line.StationCount,
		&line.PresentAlertCount, &line.CurrentAlertCount, &line.UpcomingAlertCount); err != nil {
		return LineSummary{}, err
	}
	line.Color = nullStringPointer(color)
	line.TextColor = nullStringPointer(textColor)
	return line, nil
}
