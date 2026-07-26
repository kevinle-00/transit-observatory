package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	OverallOK          = "ok"
	OverallDegraded    = "degraded"
	OverallUnavailable = "unavailable"
)

type StatusQuery struct {
	Now                 time.Time
	AlertDataMaxAge     time.Duration
	AlertCheckMaxAge    time.Duration
	GTFSDataMaxAge      time.Duration
	GTFSCheckMaxAge     time.Duration
	AlertRunMaxDuration time.Duration
	GTFSRunMaxDuration  time.Duration
	FutureTolerance     time.Duration
	RecentFailureLimit  int
}

type StatusResponse struct {
	GeneratedAt   time.Time          `json:"generated_at"`
	OverallStatus string             `json:"overall_status"`
	ServiceAlerts AlertStatusSummary `json:"service_alerts"`
	StaticGTFS    GTFSStatusSummary  `json:"static_gtfs"`
}

type StatusSection struct {
	Freshness       string            `json:"freshness"`
	DataAsOf        *time.Time        `json:"data_as_of"`
	DataAgeSeconds  *float64          `json:"data_age_seconds"`
	CheckAt         *time.Time        `json:"check_at"`
	CheckAgeSeconds *float64          `json:"check_age_seconds"`
	TimestampBasis  string            `json:"timestamp_basis"`
	Reasons         []string          `json:"reasons"`
	LastApplied     *IngestionSummary `json:"last_applied"`
	LatestAttempt   *AttemptSummary   `json:"latest_attempt"`
	RecentFailures  []FailureSummary  `json:"recent_failures"`
}

type AlertStatusSummary struct {
	StatusSection
	Counts AlertStatusCounts `json:"counts"`
}

type GTFSStatusSummary struct {
	StatusSection
	Counts GTFSStatusCounts `json:"counts"`
}

type ArchiveSummary struct {
	ObjectKey string    `json:"object_key"`
	SHA256    string    `json:"sha256"`
	Bytes     int64     `json:"bytes"`
	StoredAt  time.Time `json:"stored_at"`
	Created   *bool     `json:"created"`
}

type IngestionSummary struct {
	ID            int64           `json:"id"`
	StartedAt     time.Time       `json:"started_at"`
	CompletedAt   time.Time       `json:"completed_at"`
	RetrievedAt   *time.Time      `json:"retrieved_at"`
	DataAsOf      *time.Time      `json:"data_as_of"`
	Archive       *ArchiveSummary `json:"archive"`
	ItemCount     int             `json:"item_count"`
	TripCount     int             `json:"trip_count,omitempty"`
	StopTimeCount int             `json:"stop_time_count,omitempty"`
}

type AttemptSummary struct {
	ID              int64           `json:"id"`
	Outcome         string          `json:"outcome"`
	StartedAt       time.Time       `json:"started_at"`
	CompletedAt     *time.Time      `json:"completed_at"`
	DurationSeconds float64         `json:"duration_seconds"`
	Overdue         bool            `json:"overdue"`
	Archive         *ArchiveSummary `json:"archive"`
}

type FailureSummary struct {
	ID            int64           `json:"id"`
	FailedAt      *time.Time      `json:"failed_at"`
	Stage         string          `json:"stage"`
	Code          string          `json:"code"`
	PublicMessage string          `json:"public_message"`
	Archive       *ArchiveSummary `json:"archive"`
}

type AlertStatusCounts struct {
	Present    int `json:"present"`
	Current    int `json:"current"`
	Upcoming   int `json:"upcoming"`
	Historical int `json:"historical"`
}

type GTFSStatusCounts struct {
	Routes            int `json:"routes"`
	RegularRoutes     int `json:"regular_routes"`
	ReplacementRoutes int `json:"replacement_routes"`
	Stops             int `json:"stops"`
	Stations          int `json:"stations"`
	Relations         int `json:"relations"`
	Trips             int `json:"trips"`
	StopTimes         int `json:"stop_times"`
}

func (r *ReadRepository) GetStatus(ctx context.Context, query StatusQuery) (StatusResponse, error) {
	tx, err := r.readTx(ctx, "ingestion status query")
	if err != nil {
		return StatusResponse{}, err
	}
	defer tx.Rollback()
	query.Now = query.Now.UTC()
	if query.RecentFailureLimit < 1 {
		query.RecentFailureLimit = 1
	} else if query.RecentFailureLimit > 20 {
		query.RecentFailureLimit = 20
	}
	alerts, err := loadAlertStatus(ctx, tx, query)
	if err != nil {
		return StatusResponse{}, err
	}
	gtfsStatus, err := loadGTFSStatus(ctx, tx, query)
	if err != nil {
		return StatusResponse{}, err
	}
	if err := commitReadTx(tx, "ingestion status query"); err != nil {
		return StatusResponse{}, err
	}
	overall := OverallOK
	if alerts.Freshness == "unavailable" || gtfsStatus.Freshness == "unavailable" {
		overall = OverallUnavailable
	} else if alerts.Freshness != "fresh" || gtfsStatus.Freshness != "fresh" || len(alerts.Reasons) > 0 || len(gtfsStatus.Reasons) > 0 {
		overall = OverallDegraded
	}
	return StatusResponse{GeneratedAt: query.Now, OverallStatus: overall, ServiceAlerts: alerts, StaticGTFS: gtfsStatus}, nil
}

func loadAlertStatus(ctx context.Context, tx *sql.Tx, query StatusQuery) (AlertStatusSummary, error) {
	section := AlertStatusSummary{StatusSection: emptyStatusSection()}
	var applied appliedStatusRow
	err := tx.QueryRowContext(ctx, `
		SELECT run.id, run.started_at, run.completed_at, run.retrieved_at,
			COALESCE(run.feed_timestamp, run.retrieved_at),
			CASE WHEN run.feed_timestamp IS NULL THEN 'retrieved_at' ELSE 'feed_timestamp' END,
			COALESCE(run.alert_count, 0), archive.object_key, archive.content_sha256, archive.bytes,
			archive.stored_at, run.archive_created
		FROM ingestion_runs run LEFT JOIN raw_archives archive ON archive.id = run.raw_archive_id
		WHERE run.status = 'succeeded' AND run.alert_reconciliation_applied
		ORDER BY COALESCE(run.feed_timestamp, run.retrieved_at) DESC, run.retrieved_at DESC, run.id DESC LIMIT 1
	`).Scan(&applied.id, &applied.startedAt, &applied.completedAt, &applied.retrievedAt,
		&applied.dataAsOf, &applied.basis, &applied.itemCount, &applied.archiveKey, &applied.archiveHash,
		&applied.archiveBytes, &applied.archiveStoredAt, &applied.archiveCreated)
	if err != nil && err != sql.ErrNoRows {
		return section, fmt.Errorf("load applied service-alert ingestion: %w", err)
	}
	if err == nil {
		section.LastApplied = applied.summary()
		section.DataAsOf = nullTimePointer(applied.dataAsOf)
		section.TimestampBasis = applied.basis
		section.Freshness, section.DataAgeSeconds, section.Reasons = freshness(query.Now, section.DataAsOf, query.AlertDataMaxAge, query.FutureTolerance)
	} else {
		section.Freshness = "unavailable"
		section.Reasons = append(section.Reasons, "data_unavailable")
	}
	check, err := acceptedAlertCheck(ctx, tx)
	if err != nil {
		return section, err
	}
	applyCheck(&section.StatusSection, query.Now, check, query.AlertCheckMaxAge, query.FutureTolerance)
	if err := loadLatestAttempt(ctx, tx, "ingestion_runs", query.Now, query.AlertRunMaxDuration, &section.LatestAttempt); err != nil {
		return section, err
	}
	section.RecentFailures, err = loadFailures(ctx, tx, "ingestion_runs", query.RecentFailureLimit)
	if err != nil {
		return section, err
	}
	addOperationalReasons(&section.StatusSection, check)
	if err := tx.QueryRowContext(ctx, alertCountSQL, query.Now).Scan(&section.Counts.Present, &section.Counts.Current,
		&section.Counts.Upcoming, &section.Counts.Historical); err != nil {
		return section, fmt.Errorf("count service alerts for status: %w", err)
	}
	return section, nil
}

func loadGTFSStatus(ctx context.Context, tx *sql.Tx, query StatusQuery) (GTFSStatusSummary, error) {
	section := GTFSStatusSummary{StatusSection: emptyStatusSection()}
	var applied appliedStatusRow
	err := tx.QueryRowContext(ctx, `
		SELECT run.id, run.started_at, run.completed_at, run.retrieved_at,
			COALESCE(run.source_modified_at, run.retrieved_at),
			CASE WHEN run.source_modified_at IS NULL THEN 'retrieved_at' ELSE 'source_modified_at' END,
			COALESCE(run.route_count, 0), COALESCE(run.trip_count, 0), COALESCE(run.stop_time_count, 0),
			archive.object_key, archive.content_sha256, archive.bytes, archive.stored_at, run.archive_created
		FROM gtfs_imports run LEFT JOIN raw_archives archive ON archive.id = run.raw_archive_id
		WHERE run.status = 'succeeded' AND run.is_current LIMIT 1
	`).Scan(&applied.id, &applied.startedAt, &applied.completedAt, &applied.retrievedAt,
		&applied.dataAsOf, &applied.basis, &applied.itemCount, &applied.tripCount, &applied.stopTimeCount,
		&applied.archiveKey, &applied.archiveHash, &applied.archiveBytes, &applied.archiveStoredAt, &applied.archiveCreated)
	if err != nil && err != sql.ErrNoRows {
		return section, fmt.Errorf("load current GTFS ingestion: %w", err)
	}
	if err == nil {
		section.LastApplied = applied.summary()
		section.DataAsOf = nullTimePointer(applied.dataAsOf)
		section.TimestampBasis = applied.basis
		section.Freshness, section.DataAgeSeconds, section.Reasons = freshness(query.Now, section.DataAsOf, query.GTFSDataMaxAge, query.FutureTolerance)
	} else {
		section.Freshness = "unavailable"
		section.Reasons = append(section.Reasons, "data_unavailable")
	}
	check, err := acceptedGTFSCheck(ctx, tx)
	if err != nil {
		return section, err
	}
	applyCheck(&section.StatusSection, query.Now, check, query.GTFSCheckMaxAge, query.FutureTolerance)
	if err := loadLatestAttempt(ctx, tx, "gtfs_imports", query.Now, query.GTFSRunMaxDuration, &section.LatestAttempt); err != nil {
		return section, err
	}
	section.RecentFailures, err = loadFailures(ctx, tx, "gtfs_imports", query.RecentFailureLimit)
	if err != nil {
		return section, err
	}
	addOperationalReasons(&section.StatusSection, check)
	if err := tx.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE NOT is_replacement_bus),
		count(*) FILTER (WHERE is_replacement_bus) FROM routes`).Scan(&section.Counts.Routes,
		&section.Counts.RegularRoutes, &section.Counts.ReplacementRoutes); err != nil {
		return section, fmt.Errorf("count GTFS routes for status: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE location_type = 1) FROM stops`).Scan(
		&section.Counts.Stops, &section.Counts.Stations); err != nil {
		return section, fmt.Errorf("count GTFS stops for status: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM route_stations`).Scan(&section.Counts.Relations); err != nil {
		return section, fmt.Errorf("count GTFS relations for status: %w", err)
	}
	if section.LastApplied != nil {
		section.Counts.Trips, section.Counts.StopTimes = section.LastApplied.TripCount, section.LastApplied.StopTimeCount
	}
	return section, nil
}

type appliedStatusRow struct {
	id                                  int64
	startedAt, completedAt              time.Time
	retrievedAt, dataAsOf               sql.NullTime
	basis                               string
	itemCount, tripCount, stopTimeCount int
	archiveHash                         sql.NullString
	archiveKey                          sql.NullString
	archiveBytes                        sql.NullInt64
	archiveStoredAt                     sql.NullTime
	archiveCreated                      sql.NullBool
}

func (row appliedStatusRow) summary() *IngestionSummary {
	return &IngestionSummary{ID: row.id, StartedAt: row.startedAt, CompletedAt: row.completedAt,
		RetrievedAt: nullTimePointer(row.retrievedAt), DataAsOf: nullTimePointer(row.dataAsOf), Archive: archiveSummary(
			row.archiveKey, row.archiveHash, row.archiveBytes, row.archiveStoredAt, row.archiveCreated), ItemCount: row.itemCount,
		TripCount: row.tripCount, StopTimeCount: row.stopTimeCount}
}

func emptyStatusSection() StatusSection {
	return StatusSection{Freshness: "unknown", Reasons: []string{}, RecentFailures: []FailureSummary{}}
}

func freshness(now time.Time, timestamp *time.Time, maxAge, tolerance time.Duration) (string, *float64, []string) {
	if timestamp == nil {
		return "unknown", nil, []string{"data_timestamp_unknown"}
	}
	age := now.Sub(timestamp.UTC())
	seconds := age.Seconds()
	if age < -tolerance {
		return "unknown", nil, []string{"data_timestamp_in_future"}
	}
	if seconds < 0 {
		seconds = 0
	}
	if age > maxAge {
		return "stale", &seconds, []string{"data_stale"}
	}
	return "fresh", &seconds, []string{}
}

func applyCheck(section *StatusSection, now time.Time, check *acceptedCheckRow, maxAge, tolerance time.Duration) {
	if check != nil {
		section.CheckAt = check.retrievedAt
	}
	if section.CheckAt == nil {
		section.Reasons = append(section.Reasons, "check_unavailable")
		return
	}
	age := now.Sub(section.CheckAt.UTC())
	if age < -tolerance {
		section.Reasons = append(section.Reasons, "check_timestamp_in_future")
		return
	}
	seconds := age.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	section.CheckAgeSeconds = &seconds
	if age > maxAge {
		section.Reasons = append(section.Reasons, "check_stale")
	}
}

func acceptedAlertCheck(ctx context.Context, tx *sql.Tx) (*acceptedCheckRow, error) {
	return acceptedCheck(ctx, tx, `
		WITH accepted AS (
			SELECT id, retrieved_at, completed_at FROM ingestion_runs
			WHERE completed_at IS NOT NULL AND (
				(status = 'succeeded' AND alert_reconciliation_applied)
				OR (status = 'skipped' AND skip_code = 'duplicate')
			)
		), operational AS (
			SELECT id, completed_at FROM accepted ORDER BY completed_at DESC, id DESC LIMIT 1
		)
		SELECT (SELECT max(retrieved_at) FROM accepted), operational.completed_at, operational.id
		FROM operational
	`, "service-alert")
}

func acceptedGTFSCheck(ctx context.Context, tx *sql.Tx) (*acceptedCheckRow, error) {
	return acceptedCheck(ctx, tx, `
		WITH accepted AS (
			SELECT id, retrieved_at, completed_at FROM gtfs_imports
			WHERE completed_at IS NOT NULL AND (
				status = 'succeeded' OR (status = 'skipped' AND skip_code = 'duplicate')
			)
		), operational AS (
			SELECT id, completed_at FROM accepted ORDER BY completed_at DESC, id DESC LIMIT 1
		)
		SELECT (SELECT max(retrieved_at) FROM accepted), operational.completed_at, operational.id
		FROM operational
	`, "GTFS")
}

type acceptedCheckRow struct {
	retrievedAt *time.Time
	completedAt time.Time
	id          int64
}

func acceptedCheck(ctx context.Context, tx *sql.Tx, statement, name string) (*acceptedCheckRow, error) {
	var row acceptedCheckRow
	var retrievedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, statement).Scan(&retrievedAt, &row.completedAt, &row.id); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("load accepted %s check: %w", name, err)
	}
	row.retrievedAt = nullTimePointer(retrievedAt)
	row.completedAt = row.completedAt.UTC()
	return &row, nil
}

func loadLatestAttempt(ctx context.Context, tx *sql.Tx, table string, now time.Time, maxDuration time.Duration, target **AttemptSummary) error {
	statement := fmt.Sprintf(`SELECT run.id, run.status, run.skip_code, run.started_at, run.completed_at,
		archive.object_key, archive.content_sha256, archive.bytes, archive.stored_at, run.archive_created
		FROM %s run LEFT JOIN raw_archives archive ON archive.id = run.raw_archive_id
		ORDER BY run.started_at DESC, run.id DESC LIMIT 1`, table)
	var item AttemptSummary
	var status string
	var skipCode sql.NullString
	var completed sql.NullTime
	var hash sql.NullString
	var key sql.NullString
	var bytes sql.NullInt64
	var stored sql.NullTime
	var created sql.NullBool
	if err := tx.QueryRowContext(ctx, statement).Scan(&item.ID, &status, &skipCode, &item.StartedAt, &completed,
		&key, &hash, &bytes, &stored, &created); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return fmt.Errorf("load latest %s attempt: %w", table, err)
	}
	item.CompletedAt = nullTimePointer(completed)
	end := now
	if completed.Valid {
		end = completed.Time
	}
	item.DurationSeconds = end.Sub(item.StartedAt).Seconds()
	if item.DurationSeconds < 0 {
		item.DurationSeconds = 0
	}
	item.Overdue = status == "running" && now.Sub(item.StartedAt) > maxDuration
	switch status {
	case "succeeded":
		item.Outcome = "applied"
	case "skipped":
		item.Outcome = skipCode.String
	default:
		item.Outcome = status
	}
	item.Archive = archiveSummary(key, hash, bytes, stored, created)
	*target = &item
	return nil
}

func loadFailures(ctx context.Context, tx *sql.Tx, table string, limit int) ([]FailureSummary, error) {
	statement := fmt.Sprintf(`SELECT run.id, run.completed_at, COALESCE(run.failure_stage, ''),
		COALESCE(run.failure_code, ''), COALESCE(run.public_error_message, ''), archive.object_key, archive.content_sha256,
		archive.bytes, archive.stored_at, run.archive_created
		FROM %s run LEFT JOIN raw_archives archive ON archive.id = run.raw_archive_id
		WHERE run.status = 'failed' ORDER BY run.completed_at DESC, run.id DESC LIMIT $1`, table)
	rows, err := tx.QueryContext(ctx, statement, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent %s failures: %w", table, err)
	}
	defer rows.Close()
	result := []FailureSummary{}
	for rows.Next() {
		var item FailureSummary
		var failedAt sql.NullTime
		var hash sql.NullString
		var key sql.NullString
		var bytes sql.NullInt64
		var stored sql.NullTime
		var created sql.NullBool
		if err := rows.Scan(&item.ID, &failedAt, &item.Stage, &item.Code, &item.PublicMessage,
			&key, &hash, &bytes, &stored, &created); err != nil {
			return nil, fmt.Errorf("scan recent failure: %w", err)
		}
		item.FailedAt = nullTimePointer(failedAt)
		item.Archive = archiveSummary(key, hash, bytes, stored, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func archiveSummary(key, hash sql.NullString, bytes sql.NullInt64, stored sql.NullTime, created sql.NullBool) *ArchiveSummary {
	if !key.Valid || !hash.Valid || !bytes.Valid || !stored.Valid {
		return nil
	}
	var createdValue *bool
	if created.Valid {
		value := created.Bool
		createdValue = &value
	}
	return &ArchiveSummary{ObjectKey: key.String, SHA256: hash.String, Bytes: bytes.Int64, StoredAt: stored.Time, Created: createdValue}
}

func addOperationalReasons(section *StatusSection, check *acceptedCheckRow) {
	if section.LatestAttempt == nil {
		return
	}
	newer := attemptAfterAccepted(section.LatestAttempt.StartedAt, section.LatestAttempt.ID, check)
	if newer && section.LatestAttempt.Overdue {
		section.Reasons = append(section.Reasons, "run_overdue")
	}
	if section.LatestAttempt.Outcome == "failed" && section.LatestAttempt.CompletedAt != nil &&
		attemptAfterAccepted(*section.LatestAttempt.CompletedAt, section.LatestAttempt.ID, check) {
		section.Reasons = append(section.Reasons, "recent_failure")
		return
	}
	for _, failure := range section.RecentFailures {
		if failure.FailedAt != nil && attemptAfterAccepted(*failure.FailedAt, failure.ID, check) {
			section.Reasons = append(section.Reasons, "recent_failure")
			return
		}
	}
}

func attemptAfterAccepted(timestamp time.Time, id int64, check *acceptedCheckRow) bool {
	return check == nil || timestamp.After(check.completedAt) || (timestamp.Equal(check.completedAt) && id > check.id)
}

const alertCountSQL = `
WITH current_revisions AS (
	SELECT alert.is_present, revision.is_deleted, revision.id,
		(NOT EXISTS (SELECT 1 FROM alert_revision_active_periods p WHERE p.alert_revision_id = revision.id)
		 OR EXISTS (SELECT 1 FROM alert_revision_active_periods p WHERE p.alert_revision_id = revision.id
			AND (p.starts_at IS NULL OR p.starts_at <= $1) AND (p.ends_at IS NULL OR p.ends_at > $1))) AS active,
		EXISTS (SELECT 1 FROM alert_revision_active_periods p WHERE p.alert_revision_id = revision.id AND p.starts_at > $1) AS future
	FROM service_alerts alert JOIN service_alert_revisions revision ON revision.id = alert.current_revision_id
)
SELECT count(*) FILTER (WHERE is_present AND NOT is_deleted),
	count(*) FILTER (WHERE is_present AND NOT is_deleted AND active),
	count(*) FILTER (WHERE is_present AND NOT is_deleted AND NOT active AND future),
	count(*) FILTER (WHERE NOT is_present OR is_deleted)
FROM current_revisions`
