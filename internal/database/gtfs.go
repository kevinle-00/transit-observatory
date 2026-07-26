package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/gtfs"
	"github.com/kevinle-00/transit-observatory/internal/observability"
)

const gtfsImportLockID int64 = 746673696

type GTFSRepository struct {
	db *sql.DB
}

type IdentifierCoverage struct {
	RealtimeRouteCount int      `json:"realtime_route_count"`
	MatchedRouteCount  int      `json:"matched_route_count"`
	UnmatchedRouteIDs  []string `json:"unmatched_route_ids"`
	RealtimeStopCount  int      `json:"realtime_stop_count"`
	MatchedStopCount   int      `json:"matched_stop_count"`
	UnmatchedStopIDs   []string `json:"unmatched_stop_ids"`
}

func NewGTFSRepository(db *sql.DB) *GTFSRepository {
	return &GTFSRepository{db: db}
}

func (r *GTFSRepository) StartImport(ctx context.Context, sourceURL string) (int64, error) {
	var id int64
	if err := r.db.QueryRowContext(ctx, `
		INSERT INTO gtfs_imports (status, source_url, archive_status)
		VALUES ('running', $1, 'pending')
		RETURNING id
	`, sourceURL).Scan(&id); err != nil {
		return 0, fmt.Errorf("start GTFS import: %w", err)
	}
	return id, nil
}

func (r *GTFSRepository) SkipIfImported(ctx context.Context, importID int64, download gtfs.Download) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin GTFS duplicate check: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", gtfsImportLockID); err != nil {
		return false, fmt.Errorf("lock GTFS duplicate check: %w", err)
	}
	current, err := loadCurrentGTFSImport(ctx, tx)
	if err != nil {
		return false, err
	}
	reason := skipGTFSReason(current, download)
	if reason == "" {
		return false, nil
	}
	if err := markGTFSSkipped(ctx, tx, importID, download, reason); err != nil {
		return false, err
	}
	if err := r.commitGTFS(ctx, tx, importID, "skipped"); err != nil {
		return false, err
	}
	return true, nil
}

func (r *GTFSRepository) CompleteImport(
	ctx context.Context,
	importID int64,
	download gtfs.Download,
	dataset gtfs.Dataset,
) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin GTFS import transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", gtfsImportLockID); err != nil {
		return false, fmt.Errorf("lock GTFS import: %w", err)
	}
	current, err := loadCurrentGTFSImport(ctx, tx)
	if err != nil {
		return false, err
	}
	if reason := skipGTFSReason(current, download); reason != "" {
		if err := markGTFSSkipped(ctx, tx, importID, download, reason); err != nil {
			return false, err
		}
		if err := r.commitGTFS(ctx, tx, importID, "skipped"); err != nil {
			return false, err
		}
		return true, nil
	}

	for _, statement := range []string{
		"DELETE FROM route_stations",
		"DELETE FROM stops",
		"DELETE FROM routes",
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return false, fmt.Errorf("clear current GTFS network: %w", err)
		}
	}
	if err := insertRoutes(ctx, tx, importID, dataset.Routes); err != nil {
		return false, err
	}
	if err := insertStops(ctx, tx, importID, dataset.Stops); err != nil {
		return false, err
	}
	if err := insertRouteStations(ctx, tx, dataset.RouteStations); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE gtfs_imports SET is_current = false WHERE is_current`); err != nil {
		return false, fmt.Errorf("clear current GTFS import marker: %w", err)
	}
	summary := dataset.Summary
	result, err := tx.ExecContext(ctx, `
		UPDATE gtfs_imports
		SET status = 'succeeded', completed_at = now(), requested_at = $2,
			retrieved_at = $3, source_modified_at = $4, content_sha256 = $5,
			archive_bytes = $6, metro_archive_bytes = $7, etag = $8,
			last_modified = $9, content_type = $10, route_count = $11, stop_count = $12,
			station_count = $13, trip_count = $14, stop_time_count = $15,
			route_station_count = $16, skipped_stop_time_count = $17, is_current = true,
			skip_reason = NULL, skip_code = NULL, error_message = NULL,
			failure_stage = NULL, failure_code = NULL, public_error_message = NULL
		WHERE id = $1 AND status = 'running' AND archive_status = 'archived'
	`, importID, download.RequestedAt, download.RetrievedAt, optionalTime(download.ModifiedAt),
		download.SHA256, download.Size, summary.MetroArchiveBytes, nullableString(download.ETag),
		nullableString(download.LastModified), nullableString(download.ContentType), summary.RouteCount,
		summary.StopCount, summary.StationCount, summary.TripCount, summary.StopTimeCount,
		summary.RouteStationCount, summary.SkippedStopTimeCount)
	if err != nil {
		return false, fmt.Errorf("complete GTFS import %d: %w", importID, err)
	}
	if err := requireOneRow(result, "complete GTFS import"); err != nil {
		return false, err
	}
	if err := r.commitGTFS(ctx, tx, importID, "succeeded"); err != nil {
		return false, err
	}
	return false, nil
}

func (r *GTFSRepository) FailImport(ctx context.Context, importID int64, download *gtfs.Download, importError error) error {
	stage := "fetch"
	if download != nil {
		stage = "persist"
	}
	return r.FailImportWithFailure(ctx, importID, download, observability.Failure{
		Stage: stage, Code: "ingestion_failed", PublicMessage: "GTFS ingestion failed", Err: importError,
	})
}

func (r *GTFSRepository) FailImportWithFailure(ctx context.Context, importID int64, download *gtfs.Download, failure observability.Failure) error {
	messageRunes := []rune(failure.Error())
	if len(messageRunes) > maxStoredErrorRunes {
		messageRunes = messageRunes[:maxStoredErrorRunes]
	}
	publicMessage := truncateRunes(failure.PublicMessage, maxPublicErrorRunes)
	var requestedAt, retrievedAt, sourceModifiedAt, hash, size, etag, modified, contentType any
	if download != nil {
		requestedAt = download.RequestedAt
		retrievedAt = download.RetrievedAt
		sourceModifiedAt = optionalTime(download.ModifiedAt)
		hash = download.SHA256
		size = download.Size
		etag = nullableString(download.ETag)
		modified = nullableString(download.LastModified)
		contentType = nullableString(download.ContentType)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE gtfs_imports
		SET status = 'failed', completed_at = now(), error_message = $2,
			requested_at = $3, retrieved_at = $4, source_modified_at = $5,
			content_sha256 = $6, archive_bytes = $7, etag = $8, last_modified = $9,
			content_type = $10, failure_stage = $11, failure_code = $12,
			public_error_message = $13,
			archive_status = CASE WHEN archive_status = 'archived' THEN 'archived' ELSE 'failed' END,
			archive_error = CASE WHEN archive_status = 'archived' THEN NULL ELSE $2 END
		WHERE id = $1 AND status = 'running'
	`, importID, string(messageRunes), requestedAt, retrievedAt, sourceModifiedAt, hash, size, etag, modified,
		contentType, failure.Stage, nullableString(failure.Code), nullableString(publicMessage))
	if err != nil {
		return fmt.Errorf("mark GTFS import %d failed: %w", importID, err)
	}
	return requireOneRow(result, "mark GTFS import failed")
}

func (r *GTFSRepository) Coverage(ctx context.Context) (IdentifierCoverage, error) {
	coverage := IdentifierCoverage{UnmatchedRouteIDs: []string{}, UnmatchedStopIDs: []string{}}
	if err := r.db.QueryRowContext(ctx, `
		WITH realtime_ids AS (
			SELECT DISTINCT entity.route_id
			FROM service_alerts alert
			JOIN alert_revision_informed_entities entity
				ON entity.alert_revision_id = alert.current_revision_id
			WHERE alert.is_present AND entity.route_id IS NOT NULL
			UNION
			SELECT DISTINCT entity.trip_route_id
			FROM service_alerts alert
			JOIN alert_revision_informed_entities entity
				ON entity.alert_revision_id = alert.current_revision_id
			WHERE alert.is_present AND entity.trip_route_id IS NOT NULL
		)
		SELECT count(*), count(route.route_id)
		FROM realtime_ids realtime
		LEFT JOIN routes route ON route.route_id = realtime.route_id
	`).Scan(&coverage.RealtimeRouteCount, &coverage.MatchedRouteCount); err != nil {
		return IdentifierCoverage{}, fmt.Errorf("calculate realtime route coverage: %w", err)
	}
	if err := r.db.QueryRowContext(ctx, `
		WITH realtime_ids AS (
			SELECT DISTINCT entity.stop_id
			FROM service_alerts alert
			JOIN alert_revision_informed_entities entity
				ON entity.alert_revision_id = alert.current_revision_id
			WHERE alert.is_present AND entity.stop_id IS NOT NULL
		)
		SELECT count(*), count(stop.stop_id)
		FROM realtime_ids realtime
		LEFT JOIN stops stop ON stop.stop_id = realtime.stop_id
	`).Scan(&coverage.RealtimeStopCount, &coverage.MatchedStopCount); err != nil {
		return IdentifierCoverage{}, fmt.Errorf("calculate realtime stop coverage: %w", err)
	}
	routeRows, err := r.db.QueryContext(ctx, `
		WITH realtime_ids AS (
			SELECT entity.route_id
			FROM service_alerts alert
			JOIN alert_revision_informed_entities entity
				ON entity.alert_revision_id = alert.current_revision_id
			WHERE alert.is_present AND entity.route_id IS NOT NULL
			UNION
			SELECT entity.trip_route_id
			FROM service_alerts alert
			JOIN alert_revision_informed_entities entity
				ON entity.alert_revision_id = alert.current_revision_id
			WHERE alert.is_present AND entity.trip_route_id IS NOT NULL
		)
		SELECT realtime.route_id
		FROM realtime_ids realtime
		LEFT JOIN routes route ON route.route_id = realtime.route_id
		WHERE route.route_id IS NULL
		ORDER BY realtime.route_id
	`)
	if err != nil {
		return IdentifierCoverage{}, fmt.Errorf("query unmatched realtime routes: %w", err)
	}
	defer routeRows.Close()
	for routeRows.Next() {
		var id string
		if err := routeRows.Scan(&id); err != nil {
			return IdentifierCoverage{}, fmt.Errorf("scan unmatched realtime route: %w", err)
		}
		coverage.UnmatchedRouteIDs = append(coverage.UnmatchedRouteIDs, id)
	}
	if err := routeRows.Err(); err != nil {
		return IdentifierCoverage{}, fmt.Errorf("iterate unmatched realtime routes: %w", err)
	}
	stopRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT entity.stop_id
		FROM service_alerts alert
		JOIN alert_revision_informed_entities entity
			ON entity.alert_revision_id = alert.current_revision_id
		LEFT JOIN stops stop ON stop.stop_id = entity.stop_id
		WHERE alert.is_present AND entity.stop_id IS NOT NULL AND stop.stop_id IS NULL
		ORDER BY entity.stop_id
	`)
	if err != nil {
		return IdentifierCoverage{}, fmt.Errorf("query unmatched realtime stops: %w", err)
	}
	defer stopRows.Close()
	for stopRows.Next() {
		var id string
		if err := stopRows.Scan(&id); err != nil {
			return IdentifierCoverage{}, fmt.Errorf("scan unmatched realtime stop: %w", err)
		}
		coverage.UnmatchedStopIDs = append(coverage.UnmatchedStopIDs, id)
	}
	if err := stopRows.Err(); err != nil {
		return IdentifierCoverage{}, fmt.Errorf("iterate unmatched realtime stops: %w", err)
	}
	return coverage, nil
}

type currentGTFSImport struct {
	hash        string
	requestedAt time.Time
	retrievedAt time.Time
	modifiedAt  sql.NullTime
}

func loadCurrentGTFSImport(ctx context.Context, tx *sql.Tx) (*currentGTFSImport, error) {
	var current currentGTFSImport
	err := tx.QueryRowContext(ctx, `
		SELECT content_sha256, requested_at, retrieved_at, source_modified_at
		FROM gtfs_imports
		WHERE is_current
	`).Scan(&current.hash, &current.requestedAt, &current.retrievedAt, &current.modifiedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load current GTFS import: %w", err)
	}
	return &current, nil
}

func skipGTFSReason(current *currentGTFSImport, download gtfs.Download) string {
	if current == nil {
		return ""
	}
	if current.modifiedAt.Valid && download.ModifiedAt != nil {
		if download.ModifiedAt.Before(current.modifiedAt.Time) {
			return "source archive is older than the currently installed network"
		}
		if download.ModifiedAt.Equal(current.modifiedAt.Time) && download.RequestedAt.Before(current.requestedAt) {
			return "archive request is older than the currently installed network"
		}
	} else if download.RequestedAt.Before(current.requestedAt) {
		return "archive request is older than the currently installed network"
	}
	if current.hash == download.SHA256 {
		return "content hash matches the currently installed network"
	}
	return ""
}

func markGTFSSkipped(ctx context.Context, tx *sql.Tx, importID int64, download gtfs.Download, reason string) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE gtfs_imports
		SET status = 'skipped', completed_at = now(), requested_at = $2,
			retrieved_at = $3, source_modified_at = $4, content_sha256 = $5,
			archive_bytes = $6, etag = $7, last_modified = $8, content_type = $9,
			skip_reason = $10, skip_code = $11, error_message = NULL
		WHERE id = $1 AND status = 'running' AND archive_status = 'archived'
	`, importID, download.RequestedAt, download.RetrievedAt, optionalTime(download.ModifiedAt), download.SHA256,
		download.Size, nullableString(download.ETag), nullableString(download.LastModified), nullableString(download.ContentType),
		reason, skipCode(reason))
	if err != nil {
		return fmt.Errorf("skip duplicate GTFS import %d: %w", importID, err)
	}
	return requireOneRow(result, "skip duplicate GTFS import")
}

func insertRoutes(ctx context.Context, tx *sql.Tx, importID int64, routes []gtfs.Route) error {
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO routes (
			route_id, gtfs_import_id, agency_id, short_name, long_name,
			route_type, color, text_color, is_replacement_bus
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return fmt.Errorf("prepare route import: %w", err)
	}
	defer statement.Close()
	for _, route := range routes {
		if _, err := statement.ExecContext(ctx, route.ID, importID, nullableString(route.AgencyID),
			route.ShortName, route.LongName, route.Type, nullableString(route.Color),
			nullableString(route.TextColor), route.IsReplacementBus); err != nil {
			return fmt.Errorf("insert route %q: %w", route.ID, err)
		}
	}
	return nil
}

func insertStops(ctx context.Context, tx *sql.Tx, importID int64, stops []gtfs.Stop) error {
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO stops (
			stop_id, gtfs_import_id, name, latitude, longitude, url,
			location_type, parent_station_id, wheelchair_boarding, level_id, platform_code
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`)
	if err != nil {
		return fmt.Errorf("prepare stop import: %w", err)
	}
	defer statement.Close()
	for _, stop := range stops {
		if _, err := statement.ExecContext(ctx, stop.ID, importID, stop.Name, optionalFloatValue(stop.Latitude),
			optionalFloatValue(stop.Longitude), nullableString(stop.URL), stop.LocationType,
			nullableString(stop.ParentStationID), optionalIntValue(stop.WheelchairBoarding),
			nullableString(stop.LevelID), nullableString(stop.PlatformCode)); err != nil {
			return fmt.Errorf("insert stop %q: %w", stop.ID, err)
		}
	}
	return nil
}

func insertRouteStations(ctx context.Context, tx *sql.Tx, relations []gtfs.RouteStation) error {
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO route_stations (route_id, station_id) VALUES ($1, $2)
	`)
	if err != nil {
		return fmt.Errorf("prepare route-station import: %w", err)
	}
	defer statement.Close()
	for _, relation := range relations {
		if _, err := statement.ExecContext(ctx, relation.RouteID, relation.StationID); err != nil {
			return fmt.Errorf("insert route-station %q/%q: %w", relation.RouteID, relation.StationID, err)
		}
	}
	return nil
}

func optionalIntValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalFloatValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func (r *GTFSRepository) CurrentSummary(ctx context.Context) (gtfs.Summary, error) {
	var summary gtfs.Summary
	if err := r.db.QueryRowContext(ctx, `
		SELECT route_count, stop_count, station_count, trip_count, stop_time_count,
			route_station_count, skipped_stop_time_count, metro_archive_bytes
		FROM gtfs_imports
		WHERE is_current
	`).Scan(&summary.RouteCount, &summary.StopCount, &summary.StationCount,
		&summary.TripCount, &summary.StopTimeCount, &summary.RouteStationCount,
		&summary.SkippedStopTimeCount, &summary.MetroArchiveBytes); err != nil {
		return gtfs.Summary{}, fmt.Errorf("load current GTFS summary: %w", err)
	}
	return summary, nil
}

func (r *GTFSRepository) commitGTFS(ctx context.Context, tx *sql.Tx, importID int64, expectedStatus string) error {
	if err := tx.Commit(); err != nil {
		statusContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var status string
		statusErr := r.db.QueryRowContext(statusContext, "SELECT status FROM gtfs_imports WHERE id = $1", importID).Scan(&status)
		if statusErr == nil && status == expectedStatus {
			return nil
		}
		if statusErr != nil {
			return commitOutcomeError{commitError: err, statusError: statusErr}
		}
		return fmt.Errorf("commit GTFS import transaction: %w", err)
	}
	return nil
}
