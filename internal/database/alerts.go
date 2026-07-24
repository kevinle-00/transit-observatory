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
) error {
	feedTimestamp, err := timestampValue(summary.Timestamp)
	if err != nil {
		return fmt.Errorf("convert feed timestamp: %w", err)
	}
	hash := sha256.Sum256(result.Body)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin service-alert snapshot transaction: %w", err)
	}
	defer tx.Rollback()

	for _, alert := range summary.Alerts {
		if err := insertAlertSnapshot(ctx, tx, runID, alert); err != nil {
			return err
		}
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
			error_message = NULL
		WHERE id = $1 AND status = 'running'
	`,
		runID,
		result.RetrievedAt,
		feedTimestamp,
		result.StatusCode,
		nullableString(result.ContentType),
		len(result.Body),
		hex.EncodeToString(hash[:]),
		summary.EntityCount,
		summary.AlertCount,
	)
	if err != nil {
		return fmt.Errorf("complete service-alert ingestion run %d: %w", runID, err)
	}
	if err := requireOneRow(updateResult, "complete service-alert ingestion run"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		status, statusErr := r.runStatus(context.WithoutCancel(ctx), runID)
		if statusErr == nil && status == "succeeded" {
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

func insertAlertSnapshot(ctx context.Context, tx *sql.Tx, runID int64, alert realtime.AlertSummary) error {
	header, err := marshalTranslations(alert.Header)
	if err != nil {
		return fmt.Errorf("encode header for alert %q: %w", alert.EntityID, err)
	}
	description, err := marshalTranslations(alert.Description)
	if err != nil {
		return fmt.Errorf("encode description for alert %q: %w", alert.EntityID, err)
	}
	url, err := marshalTranslations(alert.URL)
	if err != nil {
		return fmt.Errorf("encode URL for alert %q: %w", alert.EntityID, err)
	}

	var snapshotID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO service_alert_snapshots (
			ingestion_run_id, source_entity_id, is_deleted, cause, effect, severity,
			header, description, url, unknown_fields_bytes
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
	).Scan(&snapshotID)
	if err != nil {
		return fmt.Errorf("insert service-alert snapshot %q: %w", alert.EntityID, err)
	}

	for position, period := range alert.ActivePeriods {
		startsAt, err := timestampValue(period.Start)
		if err != nil {
			return fmt.Errorf("convert start of active period %d for alert %q: %w", position, alert.EntityID, err)
		}
		endsAt, err := timestampValue(period.End)
		if err != nil {
			return fmt.Errorf("convert end of active period %d for alert %q: %w", position, alert.EntityID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alert_snapshot_active_periods (alert_snapshot_id, position, starts_at, ends_at)
			VALUES ($1, $2, $3, $4)
		`, snapshotID, position, startsAt, endsAt); err != nil {
			return fmt.Errorf("insert active period %d for alert %q: %w", position, alert.EntityID, err)
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
			return fmt.Errorf("insert informed entity %d for alert %q: %w", position, alert.EntityID, err)
		}
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
