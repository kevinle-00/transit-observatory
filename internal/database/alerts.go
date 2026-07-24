package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

const maxStoredErrorRunes = 8 << 10

const alertReconciliationLockID int64 = 74616365727473

type AlertRepository struct {
	db *sql.DB
}

func NewAlertRepository(db *sql.DB) *AlertRepository {
	return &AlertRepository{db: db}
}

func (r *AlertRepository) StartAlertRun(ctx context.Context, sourceURL string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO ingestion_runs (feed_type, status, source_url)
		VALUES ('service_alerts', 'running', $1)
		RETURNING id
	`, sourceURL).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("start service-alert ingestion run: %w", err)
	}
	return id, nil
}

func (r *AlertRepository) FailAlertRun(
	ctx context.Context,
	runID int64,
	fetch *realtime.FetchResult,
	summary *realtime.FeedSummary,
	runError error,
) error {
	messageRunes := []rune(runError.Error())
	if len(messageRunes) > maxStoredErrorRunes {
		messageRunes = messageRunes[:maxStoredErrorRunes]
	}
	message := string(messageRunes)
	var retrievedAt, httpStatus, contentType, payloadBytes, contentHash any
	if fetch != nil {
		retrievedAt = fetch.RetrievedAt
		httpStatus = fetch.StatusCode
		contentType = nullableString(fetch.ContentType)
		payloadBytes = len(fetch.Body)
		hash := sha256.Sum256(fetch.Body)
		contentHash = hex.EncodeToString(hash[:])
	}
	var entityCount, alertCount any
	if summary != nil {
		entityCount = summary.EntityCount
		alertCount = summary.AlertCount
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE ingestion_runs
		SET status = 'failed', completed_at = now(), error_message = $2,
			retrieved_at = $3, http_status = $4, content_type = $5,
			payload_bytes = $6, content_sha256 = $7,
			entity_count = $8, alert_count = $9
		WHERE id = $1 AND status = 'running'
	`, runID, message, retrievedAt, httpStatus, contentType, payloadBytes, contentHash, entityCount, alertCount)
	if err != nil {
		return fmt.Errorf("mark service-alert ingestion run %d failed: %w", runID, err)
	}
	if err := requireOneRow(result, "mark service-alert ingestion run failed"); err != nil {
		return err
	}
	return nil
}

func (r *AlertRepository) CompleteAlertRun(
	ctx context.Context,
	runID int64,
	result realtime.FetchResult,
	summary realtime.FeedSummary,
) (bool, error) {
	if summary.Incrementality != "FULL_DATASET" {
		return false, fmt.Errorf("service-alert feed incrementality %q is unsupported; FULL_DATASET is required", summary.Incrementality)
	}
	feedTimestamp, err := timestampValue(summary.Timestamp)
	if err != nil {
		return false, fmt.Errorf("convert feed timestamp: %w", err)
	}
	hash := sha256.Sum256(result.Body)
	contentHash := hex.EncodeToString(hash[:])
	observedAt, err := observationTime(summary.Timestamp, result.RetrievedAt)
	if err != nil {
		return false, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin service-alert snapshot transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", alertReconciliationLockID); err != nil {
		return false, fmt.Errorf("lock service-alert reconciliation: %w", err)
	}

	sourceURL, err := ingestionRunSourceURL(ctx, tx, runID)
	if err != nil {
		return false, err
	}
	latest, err := latestAppliedFeed(ctx, tx, runID, sourceURL)
	if err != nil {
		return false, err
	}
	if latest != nil && (latest.observedAt.After(observedAt) ||
		(latest.observedAt.Equal(observedAt) && latest.retrievedAt.After(result.RetrievedAt))) {
		if err := updateSkippedRun(
			ctx, tx, runID, result, summary, feedTimestamp, contentHash,
			"feed observation is older than the latest applied run",
		); err != nil {
			return false, err
		}
		if err := r.commitRun(ctx, tx, runID, "skipped"); err != nil {
			return false, err
		}
		return true, nil
	}
	if summary.Timestamp != nil && latest != nil && latest.contentHash == contentHash {
		if err := updateSkippedRun(
			ctx, tx, runID, result, summary, feedTimestamp, contentHash,
			"byte-identical to the latest applied payload",
		); err != nil {
			return false, err
		}
		if err := r.commitRun(ctx, tx, runID, "skipped"); err != nil {
			return false, err
		}
		return true, nil
	}

	presentEntityIDs := make([]string, 0, len(summary.Alerts))
	for _, alert := range summary.Alerts {
		presentEntityIDs = append(presentEntityIDs, alert.EntityID)
		if _, err := insertAlertSnapshot(ctx, tx, runID, alert); err != nil {
			return false, err
		}
		if err := reconcileAlert(ctx, tx, runID, sourceURL, observedAt, alert); err != nil {
			return false, err
		}
	}
	if err := closeMissingAlerts(ctx, tx, runID, sourceURL, observedAt, presentEntityIDs); err != nil {
		return false, err
	}

	updateResult, err := tx.ExecContext(ctx, `
		UPDATE ingestion_runs
		SET status = 'succeeded',
			completed_at = now(),
			retrieved_at = $2,
			feed_timestamp = $3,
			http_status = $4,
			content_type = $5,
			payload_bytes = $6,
			content_sha256 = $7,
			entity_count = $8,
			alert_count = $9,
			skip_reason = NULL,
			alert_reconciliation_applied = true,
			error_message = NULL
		WHERE id = $1 AND status = 'running'
	`,
		runID,
		result.RetrievedAt,
		feedTimestamp,
		result.StatusCode,
		nullableString(result.ContentType),
		len(result.Body),
		contentHash,
		summary.EntityCount,
		summary.AlertCount,
	)
	if err != nil {
		return false, fmt.Errorf("complete service-alert ingestion run %d: %w", runID, err)
	}
	if err := requireOneRow(updateResult, "complete service-alert ingestion run"); err != nil {
		return false, err
	}
	if err := r.commitRun(ctx, tx, runID, "succeeded"); err != nil {
		return false, err
	}
	return false, nil
}

type appliedFeed struct {
	contentHash string
	observedAt  time.Time
	retrievedAt time.Time
}

func ingestionRunSourceURL(ctx context.Context, tx *sql.Tx, runID int64) (string, error) {
	var sourceURL string
	if err := tx.QueryRowContext(ctx, `
		SELECT source_url FROM ingestion_runs WHERE id = $1 AND status = 'running'
	`, runID).Scan(&sourceURL); err != nil {
		return "", fmt.Errorf("load source URL for ingestion run %d: %w", runID, err)
	}
	return sourceURL, nil
}

func latestAppliedFeed(
	ctx context.Context,
	tx *sql.Tx,
	runID int64,
	sourceURL string,
) (*appliedFeed, error) {
	var latest appliedFeed
	err := tx.QueryRowContext(ctx, `
		SELECT content_sha256, COALESCE(feed_timestamp, retrieved_at), retrieved_at
		FROM ingestion_runs
		WHERE id <> $1
			AND source_url = $2
			AND status = 'succeeded'
			AND alert_reconciliation_applied
		ORDER BY COALESCE(feed_timestamp, retrieved_at) DESC, retrieved_at DESC, id DESC
		LIMIT 1
	`, runID, sourceURL).Scan(&latest.contentHash, &latest.observedAt, &latest.retrievedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load latest applied service-alert feed: %w", err)
	}
	return &latest, nil
}

func updateSkippedRun(
	ctx context.Context,
	tx *sql.Tx,
	runID int64,
	result realtime.FetchResult,
	summary realtime.FeedSummary,
	feedTimestamp any,
	contentHash string,
	reason string,
) error {
	updateResult, err := tx.ExecContext(ctx, `
		UPDATE ingestion_runs
		SET status = 'skipped',
			completed_at = now(),
			retrieved_at = $2,
			feed_timestamp = $3,
			http_status = $4,
			content_type = $5,
			payload_bytes = $6,
			content_sha256 = $7,
			entity_count = $8,
			alert_count = $9,
			skip_reason = $10,
			error_message = NULL
		WHERE id = $1 AND status = 'running'
	`,
		runID,
		result.RetrievedAt,
		feedTimestamp,
		result.StatusCode,
		nullableString(result.ContentType),
		len(result.Body),
		contentHash,
		summary.EntityCount,
		summary.AlertCount,
		reason,
	)
	if err != nil {
		return fmt.Errorf("skip duplicate service-alert ingestion run %d: %w", runID, err)
	}
	return requireOneRow(updateResult, "skip duplicate service-alert ingestion run")
}

func (r *AlertRepository) commitRun(ctx context.Context, tx *sql.Tx, runID int64, expectedStatus string) error {
	if err := tx.Commit(); err != nil {
		status, statusErr := r.runStatus(context.WithoutCancel(ctx), runID)
		if statusErr == nil && status == expectedStatus {
			return nil
		}
		if statusErr != nil {
			return commitOutcomeError{commitError: err, statusError: statusErr}
		}
		return fmt.Errorf("commit service-alert snapshot transaction: %w", err)
	}
	return nil
}

func (r *AlertRepository) runStatus(ctx context.Context, runID int64) (string, error) {
	statusContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var status string
	if err := r.db.QueryRowContext(statusContext, "SELECT status FROM ingestion_runs WHERE id = $1", runID).Scan(&status); err != nil {
		return "", fmt.Errorf("resolve commit outcome for ingestion run %d: %w", runID, err)
	}
	return status, nil
}

type commitOutcomeError struct {
	commitError error
	statusError error
}

func (e commitOutcomeError) Error() string {
	return fmt.Sprintf("service-alert snapshot commit outcome is unknown: commit: %v; status check: %v", e.commitError, e.statusError)
}

func (e commitOutcomeError) Unwrap() error {
	return e.commitError
}

func (e commitOutcomeError) CommitOutcomeUnknown() bool {
	return true
}

func insertAlertSnapshot(ctx context.Context, tx *sql.Tx, runID int64, alert realtime.AlertSummary) (int64, error) {
	header, err := marshalTranslations(alert.Header)
	if err != nil {
		return 0, fmt.Errorf("encode header for alert %q: %w", alert.EntityID, err)
	}
	description, err := marshalTranslations(alert.Description)
	if err != nil {
		return 0, fmt.Errorf("encode description for alert %q: %w", alert.EntityID, err)
	}
	url, err := marshalTranslations(alert.URL)
	if err != nil {
		return 0, fmt.Errorf("encode URL for alert %q: %w", alert.EntityID, err)
	}

	var snapshotID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO service_alert_snapshots (
			ingestion_run_id, source_entity_id, is_deleted, cause, effect, severity,
			header, description, url, unknown_fields_bytes, unknown_fields_sha256
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`,
		runID,
		alert.EntityID,
		alert.Deleted,
		optionalString(alert.Cause),
		optionalString(alert.Effect),
		optionalString(alert.Severity),
		header,
		description,
		url,
		alert.UnknownFieldsBytes,
		nullableString(alert.UnknownFieldsHash),
	).Scan(&snapshotID)
	if err != nil {
		return 0, fmt.Errorf("insert service-alert snapshot %q: %w", alert.EntityID, err)
	}

	for position, period := range alert.ActivePeriods {
		startsAt, err := timestampValue(period.Start)
		if err != nil {
			return 0, fmt.Errorf("convert start of active period %d for alert %q: %w", position, alert.EntityID, err)
		}
		endsAt, err := timestampValue(period.End)
		if err != nil {
			return 0, fmt.Errorf("convert end of active period %d for alert %q: %w", position, alert.EntityID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alert_snapshot_active_periods (alert_snapshot_id, position, starts_at, ends_at)
			VALUES ($1, $2, $3, $4)
		`, snapshotID, position, startsAt, endsAt); err != nil {
			return 0, fmt.Errorf("insert active period %d for alert %q: %w", position, alert.EntityID, err)
		}
	}

	for position, entity := range alert.InformedEntities {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alert_snapshot_informed_entities (
				alert_snapshot_id, position, agency_id, route_id, route_type, stop_id,
				direction_id, trip_id, trip_route_id, trip_start_time, trip_start_date,
				trip_direction_id, trip_schedule_relationship
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`,
			snapshotID,
			position,
			nullableString(entity.AgencyID),
			nullableString(entity.RouteID),
			optionalInt32(entity.RouteType),
			nullableString(entity.StopID),
			optionalUint32(entity.DirectionID),
			nullableString(entity.TripID),
			nullableString(entity.TripRouteID),
			nullableString(entity.TripStartTime),
			nullableString(entity.TripStartDate),
			optionalUint32(entity.TripDirection),
			nullableString(entity.TripSchedule),
		); err != nil {
			return 0, fmt.Errorf("insert informed entity %d for alert %q: %w", position, alert.EntityID, err)
		}
	}
	return snapshotID, nil
}

func reconcileAlert(
	ctx context.Context,
	tx *sql.Tx,
	runID int64,
	sourceURL string,
	observedAt time.Time,
	alert realtime.AlertSummary,
) error {
	contentHash, err := realtime.HashAlert(alert)
	if err != nil {
		return fmt.Errorf("hash service alert %q: %w", alert.EntityID, err)
	}

	var alertID int64
	var isPresent bool
	var currentRevisionID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id, is_present, current_revision_id
		FROM service_alerts
		WHERE source_url = $1 AND source_entity_id = $2
		FOR UPDATE
	`, sourceURL, alert.EntityID).Scan(&alertID, &isPresent, &currentRevisionID)
	if err == sql.ErrNoRows {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO service_alerts (
				source_url, source_entity_id, first_seen_at, last_seen_at,
				first_seen_run_id, last_seen_run_id, is_present
			)
			VALUES ($1, $2, $3, $3, $4, $4, true)
			RETURNING id
		`, sourceURL, alert.EntityID, observedAt, runID).Scan(&alertID); err != nil {
			return fmt.Errorf("insert service alert identity %q: %w", alert.EntityID, err)
		}
		revisionID, err := insertAlertRevision(ctx, tx, alertID, 1, runID, observedAt, contentHash, alert)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE service_alerts SET current_revision_id = $2 WHERE id = $1
		`, alertID, revisionID); err != nil {
			return fmt.Errorf("set initial revision for alert %q: %w", alert.EntityID, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load service alert identity %q: %w", alert.EntityID, err)
	}
	if !currentRevisionID.Valid {
		return fmt.Errorf("service alert %q has no current revision", alert.EntityID)
	}

	if isPresent {
		var currentHash string
		if err := tx.QueryRowContext(ctx, `
			SELECT content_sha256 FROM service_alert_revisions WHERE id = $1
		`, currentRevisionID.Int64).Scan(&currentHash); err != nil {
			return fmt.Errorf("load current revision for alert %q: %w", alert.EntityID, err)
		}
		if currentHash == contentHash {
			if _, err := tx.ExecContext(ctx, `
				UPDATE service_alert_revisions SET last_seen_at = $2 WHERE id = $1
			`, currentRevisionID.Int64, observedAt); err != nil {
				return fmt.Errorf("update unchanged revision for alert %q: %w", alert.EntityID, err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE service_alerts
				SET last_seen_at = $2, last_seen_run_id = $3, closed_at = NULL
				WHERE id = $1
			`, alertID, observedAt, runID); err != nil {
				return fmt.Errorf("update observation for alert %q: %w", alert.EntityID, err)
			}
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE service_alert_revisions
			SET closed_at = $2, closed_run_id = $3
			WHERE id = $1 AND closed_at IS NULL
		`, currentRevisionID.Int64, observedAt, runID); err != nil {
			return fmt.Errorf("close changed revision for alert %q: %w", alert.EntityID, err)
		}
	}

	var nextRevision int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(max(revision_number), 0) + 1
		FROM service_alert_revisions
		WHERE service_alert_id = $1
	`, alertID).Scan(&nextRevision); err != nil {
		return fmt.Errorf("calculate next revision for alert %q: %w", alert.EntityID, err)
	}
	revisionID, err := insertAlertRevision(ctx, tx, alertID, nextRevision, runID, observedAt, contentHash, alert)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE service_alerts
		SET last_seen_at = $2,
			last_seen_run_id = $3,
			is_present = true,
			closed_at = NULL,
			current_revision_id = $4
		WHERE id = $1
	`, alertID, observedAt, runID, revisionID); err != nil {
		return fmt.Errorf("activate new revision for alert %q: %w", alert.EntityID, err)
	}
	return nil
}

func insertAlertRevision(
	ctx context.Context,
	tx *sql.Tx,
	alertID int64,
	revisionNumber int,
	runID int64,
	observedAt time.Time,
	contentHash string,
	alert realtime.AlertSummary,
) (int64, error) {
	header, err := marshalTranslations(alert.Header)
	if err != nil {
		return 0, fmt.Errorf("encode revision header for alert %q: %w", alert.EntityID, err)
	}
	description, err := marshalTranslations(alert.Description)
	if err != nil {
		return 0, fmt.Errorf("encode revision description for alert %q: %w", alert.EntityID, err)
	}
	url, err := marshalTranslations(alert.URL)
	if err != nil {
		return 0, fmt.Errorf("encode revision URL for alert %q: %w", alert.EntityID, err)
	}

	var revisionID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO service_alert_revisions (
			service_alert_id, revision_number, content_sha256, is_deleted,
			cause, effect, severity, header, description, url,
			unknown_fields_bytes, unknown_fields_sha256,
			first_seen_at, last_seen_at, opened_run_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13, $14)
		RETURNING id
	`,
		alertID,
		revisionNumber,
		contentHash,
		alert.Deleted,
		optionalString(alert.Cause),
		optionalString(alert.Effect),
		optionalString(alert.Severity),
		header,
		description,
		url,
		alert.UnknownFieldsBytes,
		nullableString(alert.UnknownFieldsHash),
		observedAt,
		runID,
	).Scan(&revisionID); err != nil {
		return 0, fmt.Errorf("insert revision %d for alert %q: %w", revisionNumber, alert.EntityID, err)
	}

	for position, period := range alert.ActivePeriods {
		startsAt, err := timestampValue(period.Start)
		if err != nil {
			return 0, fmt.Errorf("convert revision active-period start for alert %q: %w", alert.EntityID, err)
		}
		endsAt, err := timestampValue(period.End)
		if err != nil {
			return 0, fmt.Errorf("convert revision active-period end for alert %q: %w", alert.EntityID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alert_revision_active_periods (alert_revision_id, position, starts_at, ends_at)
			VALUES ($1, $2, $3, $4)
		`, revisionID, position, startsAt, endsAt); err != nil {
			return 0, fmt.Errorf("insert revision active period for alert %q: %w", alert.EntityID, err)
		}
	}
	for position, entity := range alert.InformedEntities {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alert_revision_informed_entities (
				alert_revision_id, position, agency_id, route_id, route_type, stop_id,
				direction_id, trip_id, trip_route_id, trip_start_time, trip_start_date,
				trip_direction_id, trip_schedule_relationship
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`,
			revisionID,
			position,
			nullableString(entity.AgencyID),
			nullableString(entity.RouteID),
			optionalInt32(entity.RouteType),
			nullableString(entity.StopID),
			optionalUint32(entity.DirectionID),
			nullableString(entity.TripID),
			nullableString(entity.TripRouteID),
			nullableString(entity.TripStartTime),
			nullableString(entity.TripStartDate),
			optionalUint32(entity.TripDirection),
			nullableString(entity.TripSchedule),
		); err != nil {
			return 0, fmt.Errorf("insert revision informed entity for alert %q: %w", alert.EntityID, err)
		}
	}
	return revisionID, nil
}

func closeMissingAlerts(
	ctx context.Context,
	tx *sql.Tx,
	runID int64,
	sourceURL string,
	observedAt time.Time,
	presentEntityIDs []string,
) error {
	encodedIDs, err := json.Marshal(presentEntityIDs)
	if err != nil {
		return fmt.Errorf("encode present service-alert IDs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE service_alert_revisions AS revision
		SET closed_at = $1, closed_run_id = $2
		FROM service_alerts AS alert
		WHERE revision.id = alert.current_revision_id
			AND alert.source_url = $4
			AND alert.is_present
			AND NOT EXISTS (
				SELECT 1
				FROM jsonb_array_elements_text($3::jsonb) AS present(source_entity_id)
				WHERE present.source_entity_id = alert.source_entity_id
			)
	`, observedAt, runID, string(encodedIDs), sourceURL); err != nil {
		return fmt.Errorf("close revisions missing from service-alert feed: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE service_alerts AS alert
		SET is_present = false, closed_at = $1
		WHERE alert.source_url = $3
			AND alert.is_present
			AND NOT EXISTS (
				SELECT 1
				FROM jsonb_array_elements_text($2::jsonb) AS present(source_entity_id)
				WHERE present.source_entity_id = alert.source_entity_id
			)
	`, observedAt, string(encodedIDs), sourceURL); err != nil {
		return fmt.Errorf("close alerts missing from service-alert feed: %w", err)
	}
	return nil
}

func marshalTranslations(value []realtime.Translation) (string, error) {
	if value == nil {
		value = []realtime.Translation{}
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func timestampValue(value *realtime.Timestamp) (any, error) {
	if value == nil {
		return nil, nil
	}
	if value.Unix > math.MaxInt64 {
		return nil, fmt.Errorf("Unix timestamp %d exceeds int64", value.Unix)
	}
	return time.Unix(int64(value.Unix), 0).UTC(), nil
}

func observationTime(value *realtime.Timestamp, fallback time.Time) (time.Time, error) {
	if value == nil {
		return fallback.UTC(), nil
	}
	if value.Unix > math.MaxInt64 {
		return time.Time{}, fmt.Errorf("feed Unix timestamp %d exceeds int64", value.Unix)
	}
	return time.Unix(int64(value.Unix), 0).UTC(), nil
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func optionalInt32(value *int32) any {
	if value == nil {
		return nil
	}
	return int64(*value)
}

func optionalUint32(value *uint32) any {
	if value == nil {
		return nil
	}
	return int64(*value)
}

func requireOneRow(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: read affected rows: %w", operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s: expected one running ingestion run, affected %d", operation, rows)
	}
	return nil
}
