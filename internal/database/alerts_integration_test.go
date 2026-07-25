package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

func TestAlertRepositoryStoresCompleteSnapshot(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()

	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() error = %v", err)
	}
	result := realtime.FetchResult{
		Body:        []byte("protobuf payload"),
		StatusCode:  200,
		ContentType: "application/octet-stream",
		RetrievedAt: time.Date(2026, time.July, 24, 9, 0, 0, 0, time.UTC),
	}
	summary := testSummary()
	if _, err := repository.CompleteAlertRun(ctx, runID, result, summary); err != nil {
		t.Fatalf("CompleteAlertRun() error = %v", err)
	}

	var status string
	var alertCount, payloadBytes int
	if err := db.QueryRowContext(ctx, `
		SELECT status, alert_count, payload_bytes
		FROM ingestion_runs
		WHERE id = $1
	`, runID).Scan(&status, &alertCount, &payloadBytes); err != nil {
		t.Fatalf("query ingestion run: %v", err)
	}
	if status != "succeeded" || alertCount != 1 || payloadBytes != len(result.Body) {
		t.Errorf("run status = %q, alert count = %d, payload bytes = %d", status, alertCount, payloadBytes)
	}

	assertCount(t, db, "service_alert_snapshots", 1)
	assertCount(t, db, "alert_snapshot_active_periods", 1)
	assertCount(t, db, "alert_snapshot_informed_entities", 1)

	var routeID, stopID string
	var directionID int64
	if err := db.QueryRowContext(ctx, `
		SELECT route_id, stop_id, direction_id
		FROM alert_snapshot_informed_entities
	`).Scan(&routeID, &stopID, &directionID); err != nil {
		t.Fatalf("query informed entity: %v", err)
	}
	if routeID != "aus:vic:vic-02-FKN:" || stopID != "vic:rail:ARM" || directionID != 0 {
		t.Errorf("informed entity = route %q, stop %q, direction %d", routeID, stopID, directionID)
	}
}

func TestAlertRepositoryRollsBackPartialSnapshot(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()

	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() error = %v", err)
	}
	summary := testSummary()
	summary.Alerts = append(summary.Alerts, summary.Alerts[0])
	summary.AlertCount = 2
	result := realtime.FetchResult{Body: []byte("payload"), StatusCode: 200, RetrievedAt: time.Now()}
	if _, err := repository.CompleteAlertRun(ctx, runID, result, summary); err == nil {
		t.Fatal("CompleteAlertRun() error = nil, want duplicate entity failure")
	}
	assertCount(t, db, "service_alert_snapshots", 0)

	if err := repository.FailAlertRun(ctx, runID, &result, &summary, sql.ErrTxDone); err != nil {
		t.Fatalf("FailAlertRun() error = %v", err)
	}
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM ingestion_runs WHERE id = $1", runID).Scan(&status); err != nil {
		t.Fatalf("query failed run: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	var payloadBytes, entityCount int
	if err := db.QueryRowContext(ctx, `
		SELECT payload_bytes, entity_count
		FROM ingestion_runs
		WHERE id = $1
	`, runID).Scan(&payloadBytes, &entityCount); err != nil {
		t.Fatalf("query failed run metadata: %v", err)
	}
	if payloadBytes != len(result.Body) || entityCount != summary.EntityCount {
		t.Errorf("failed metadata = payload bytes %d, entity count %d", payloadBytes, entityCount)
	}
}

func TestAlertRepositoryTracksAlertLifecycle(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	base := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)

	first := testSummaryAt(base)
	completeTestRun(t, repository, "payload-1", first, false)

	unchanged := testSummaryAt(base.Add(time.Minute))
	completeTestRun(t, repository, "payload-2", unchanged, false)

	changed := testSummaryAt(base.Add(2 * time.Minute))
	changed.Alerts[0].Description[0].Text = "Trains now delayed by thirty minutes"
	completeTestRun(t, repository, "payload-3", changed, false)

	missing := realtime.FeedSummary{
		Incrementality: "FULL_DATASET",
		Timestamp:      &realtime.Timestamp{Unix: uint64(base.Add(3 * time.Minute).Unix())},
		EntityCount:    0,
		AlertCount:     0,
		Alerts:         []realtime.AlertSummary{},
	}
	completeTestRun(t, repository, "payload-4", missing, false)

	reappeared := testSummaryAt(base.Add(4 * time.Minute))
	reappeared.Alerts[0].Description[0].Text = "Trains now delayed by thirty minutes"
	completeTestRun(t, repository, "payload-5", reappeared, false)
	duplicateRunID := completeTestRun(t, repository, "payload-5", reappeared, true)

	var status, skipReason string
	if err := db.QueryRowContext(ctx, `
		SELECT status, skip_reason FROM ingestion_runs WHERE id = $1
	`, duplicateRunID).Scan(&status, &skipReason); err != nil {
		t.Fatalf("query skipped run: %v", err)
	}
	if status != "skipped" || skipReason == "" {
		t.Errorf("duplicate run = status %q, reason %q", status, skipReason)
	}
	var skippedSnapshots int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM service_alert_snapshots WHERE ingestion_run_id = $1
	`, duplicateRunID).Scan(&skippedSnapshots); err != nil {
		t.Fatalf("count skipped snapshots: %v", err)
	}
	if skippedSnapshots != 0 {
		t.Errorf("skipped snapshot count = %d, want 0", skippedSnapshots)
	}

	var firstSeen, lastSeen time.Time
	var isPresent bool
	if err := db.QueryRowContext(ctx, `
		SELECT first_seen_at, last_seen_at, is_present
		FROM service_alerts
		WHERE source_entity_id = 'alert-1'
	`).Scan(&firstSeen, &lastSeen, &isPresent); err != nil {
		t.Fatalf("query service alert: %v", err)
	}
	if !firstSeen.Equal(base) || !lastSeen.Equal(base.Add(4*time.Minute)) || !isPresent {
		t.Errorf("alert lifecycle = first %s, last %s, present %t", firstSeen, lastSeen, isPresent)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT revision_number, first_seen_at, last_seen_at, closed_at
		FROM service_alert_revisions
		ORDER BY revision_number
	`)
	if err != nil {
		t.Fatalf("query revisions: %v", err)
	}
	defer rows.Close()
	type revision struct {
		number    int
		firstSeen time.Time
		lastSeen  time.Time
		closedAt  sql.NullTime
	}
	var revisions []revision
	for rows.Next() {
		var item revision
		if err := rows.Scan(&item.number, &item.firstSeen, &item.lastSeen, &item.closedAt); err != nil {
			t.Fatalf("scan revision: %v", err)
		}
		revisions = append(revisions, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate revisions: %v", err)
	}
	if len(revisions) != 3 {
		t.Fatalf("revision count = %d, want 3", len(revisions))
	}
	if !revisions[0].firstSeen.Equal(base) ||
		!revisions[0].lastSeen.Equal(base.Add(time.Minute)) ||
		!revisions[0].closedAt.Valid ||
		!revisions[0].closedAt.Time.Equal(base.Add(2*time.Minute)) {
		t.Errorf("revision 1 = %#v", revisions[0])
	}
	if !revisions[1].closedAt.Valid || !revisions[1].closedAt.Time.Equal(base.Add(3*time.Minute)) {
		t.Errorf("revision 2 = %#v", revisions[1])
	}
	if revisions[2].closedAt.Valid || !revisions[2].firstSeen.Equal(base.Add(4*time.Minute)) {
		t.Errorf("revision 3 = %#v", revisions[2])
	}

	failedRunID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() for failure error = %v", err)
	}
	if err := repository.FailAlertRun(ctx, failedRunID, nil, nil, context.DeadlineExceeded); err != nil {
		t.Fatalf("FailAlertRun() error = %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT is_present FROM service_alerts WHERE source_entity_id = 'alert-1'
	`).Scan(&isPresent); err != nil {
		t.Fatalf("query alert after failed run: %v", err)
	}
	if !isPresent {
		t.Error("failed run incorrectly closed the current alert")
	}
}

func TestAlertRepositoryReappliesHistoricalPayloadAfterInterveningState(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	base := time.Date(2026, time.July, 24, 11, 0, 0, 0, time.UTC)

	stateA := testSummaryAt(base)
	completeTestRun(t, repository, "payload-a", stateA, false)

	stateB := testSummaryAt(base.Add(time.Minute))
	stateB.Alerts[0].Description[0].Text = "State B"
	completeTestRun(t, repository, "payload-b", stateB, false)

	stateAAgain := testSummaryAt(base.Add(2 * time.Minute))
	completeTestRun(t, repository, "payload-a", stateAAgain, false)

	assertCount(t, db, "service_alert_revisions", 3)
}

func TestAlertRepositorySkipsStaleObservation(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	base := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)

	newer := testSummaryAt(base)
	completeTestRun(t, repository, "newer-payload", newer, false)

	older := testSummaryAt(base.Add(-time.Minute))
	older.Alerts[0].Description[0].Text = "Stale content"
	completeTestRun(t, repository, "older-payload", older, true)

	assertCount(t, db, "service_alert_revisions", 1)
	var lastSeen time.Time
	if err := db.QueryRow(`SELECT last_seen_at FROM service_alerts WHERE source_entity_id = 'alert-1'`).Scan(&lastSeen); err != nil {
		t.Fatalf("query last seen timestamp: %v", err)
	}
	if !lastSeen.Equal(base) {
		t.Errorf("last seen = %s, want %s", lastSeen, base)
	}
}

func TestAlertRepositoryScopesIdentityAndClosureToSource(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	base := time.Date(2026, time.July, 24, 13, 0, 0, 0, time.UTC)

	completeTestRunForSource(t, repository, "https://source-a.test/alerts", "source-a", testSummaryAt(base), false)
	secondSource := testSummaryAt(base)
	secondSource.Alerts[0].Description[0].Text = "Independent source"
	completeTestRunForSource(t, repository, "https://source-b.test/alerts", "source-b", secondSource, false)

	emptySecondSource := realtime.FeedSummary{
		Incrementality: "FULL_DATASET",
		Timestamp:      &realtime.Timestamp{Unix: uint64(base.Add(time.Minute).Unix())},
		Alerts:         []realtime.AlertSummary{},
	}
	completeTestRunForSource(t, repository, "https://source-b.test/alerts", "source-b-empty", emptySecondSource, false)

	var firstPresent, secondPresent bool
	if err := db.QueryRow(`
		SELECT is_present FROM service_alerts
		WHERE source_url = 'https://source-a.test/alerts' AND source_entity_id = 'alert-1'
	`).Scan(&firstPresent); err != nil {
		t.Fatalf("query first source: %v", err)
	}
	if err := db.QueryRow(`
		SELECT is_present FROM service_alerts
		WHERE source_url = 'https://source-b.test/alerts' AND source_entity_id = 'alert-1'
	`).Scan(&secondPresent); err != nil {
		t.Fatalf("query second source: %v", err)
	}
	if !firstPresent || secondPresent {
		t.Errorf("source presence = first %t, second %t", firstPresent, secondPresent)
	}
}

func TestAlertRepositoryRejectsDifferentialFeed(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() error = %v", err)
	}
	summary := testSummary()
	summary.Incrementality = "DIFFERENTIAL"
	_, err = repository.CompleteAlertRun(ctx, runID, realtime.FetchResult{Body: []byte("delta")}, summary)
	if err == nil {
		t.Fatal("CompleteAlertRun() error = nil, want differential-feed rejection")
	}
	assertCount(t, db, "service_alerts", 0)
}

func TestAlertRepositoryInitializesAfterPreRevisionSuccessfulRun(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	base := time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC)
	payload := []byte("pre-revision-payload")
	hash := sha256.Sum256(payload)
	if _, err := db.Exec(`
		INSERT INTO ingestion_runs (
			feed_type, status, source_url, completed_at, retrieved_at,
			feed_timestamp, content_sha256, alert_reconciliation_applied
		)
		VALUES ('service_alerts', 'succeeded', 'https://example.test/alerts',
			$1, $1, $1, $2, false)
	`, base, hex.EncodeToString(hash[:])); err != nil {
		t.Fatalf("insert pre-revision run: %v", err)
	}

	completeTestRunAt(t, repository, "https://example.test/alerts", string(payload), testSummaryAt(base), base.Add(time.Minute), false)
	assertCount(t, db, "service_alerts", 1)
	assertCount(t, db, "service_alert_revisions", 1)
}

func TestAlertRepositoryUsesRetrievalTimeToBreakTimestampTie(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	base := time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)

	newer := testSummaryAt(base)
	completeTestRunAt(t, repository, "https://example.test/alerts", "newer", newer, base.Add(2*time.Minute), false)
	older := testSummaryAt(base)
	older.Alerts[0].Description[0].Text = "Older response with same feed timestamp"
	completeTestRunAt(t, repository, "https://example.test/alerts", "older", older, base.Add(time.Minute), true)

	assertCount(t, db, "service_alert_revisions", 1)
}

func TestAlertRepositorySelectsLatestBaselineByObservationNotRunID(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	base := time.Date(2026, time.July, 24, 15, 30, 0, 0, time.UTC)

	olderRunID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("start lower-ID newer-observation run: %v", err)
	}
	newerRunID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("start higher-ID older-observation run: %v", err)
	}

	olderObservation := testSummaryAt(base)
	completeExistingTestRun(t, repository, newerRunID, "older-observation", olderObservation, base, false)
	newerObservation := testSummaryAt(base.Add(2 * time.Minute))
	newerObservation.Alerts[0].Description[0].Text = "Newest applied state"
	completeExistingTestRun(t, repository, olderRunID, "newer-observation", newerObservation, base.Add(2*time.Minute), false)

	between := testSummaryAt(base.Add(time.Minute))
	between.Alerts[0].Description[0].Text = "Stale intermediate state"
	completeTestRunAt(t, repository, "https://example.test/alerts", "between", between, base.Add(3*time.Minute), true)

	assertCount(t, db, "service_alert_revisions", 2)
}

func TestAlertRepositoryObservesTimestampLessRepeatedPayload(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	base := time.Date(2026, time.July, 24, 16, 0, 0, 0, time.UTC)
	summary := testSummary()
	summary.Timestamp = nil

	completeTestRunAt(t, repository, "https://example.test/alerts", "same-body", summary, base, false)
	completeTestRunAt(t, repository, "https://example.test/alerts", "same-body", summary, base.Add(time.Minute), false)

	assertCount(t, db, "service_alert_snapshots", 2)
	assertCount(t, db, "service_alert_revisions", 1)
	var lastSeen time.Time
	if err := db.QueryRow(`SELECT last_seen_at FROM service_alerts WHERE source_entity_id = 'alert-1'`).Scan(&lastSeen); err != nil {
		t.Fatalf("query timestamp-less alert: %v", err)
	}
	if !lastSeen.Equal(base.Add(time.Minute)) {
		t.Errorf("last seen = %s, want %s", lastSeen, base.Add(time.Minute))
	}
}

func integrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if _, err := db.Exec(`TRUNCATE ingestion_runs, gtfs_imports RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset integration database: %v", err)
	}
	return db
}

func testSummary() realtime.FeedSummary {
	cause := "CONSTRUCTION"
	effect := "MODIFIED_SERVICE"
	direction := uint32(0)
	return realtime.FeedSummary{
		Incrementality: "FULL_DATASET",
		Timestamp:      &realtime.Timestamp{Unix: 1784883600, UTC: "2026-07-24T09:00:00Z"},
		EntityCount:    1,
		AlertCount:     1,
		Alerts: []realtime.AlertSummary{
			{
				EntityID:    "alert-1",
				Cause:       &cause,
				Effect:      &effect,
				Header:      []realtime.Translation{{Text: "Planned work", Language: "en"}},
				Description: []realtime.Translation{{Text: "Buses replace trains", Language: "en"}},
				ActivePeriods: []realtime.ActivePeriod{
					{
						Start: &realtime.Timestamp{Unix: 1784883600},
						End:   &realtime.Timestamp{Unix: 1784890800},
					},
				},
				InformedEntities: []realtime.InformedEntity{
					{
						AgencyID:    "1",
						RouteID:     "aus:vic:vic-02-FKN:",
						StopID:      "vic:rail:ARM",
						DirectionID: &direction,
					},
				},
			},
		},
	}
}

func testSummaryAt(observedAt time.Time) realtime.FeedSummary {
	summary := testSummary()
	summary.Timestamp = &realtime.Timestamp{Unix: uint64(observedAt.Unix()), UTC: observedAt.Format(time.RFC3339)}
	return summary
}

func completeTestRun(
	t *testing.T,
	repository *AlertRepository,
	payload string,
	summary realtime.FeedSummary,
	wantSkipped bool,
) int64 {
	return completeTestRunForSource(t, repository, "https://example.test/alerts", payload, summary, wantSkipped)
}

func completeTestRunForSource(
	t *testing.T,
	repository *AlertRepository,
	sourceURL string,
	payload string,
	summary realtime.FeedSummary,
	wantSkipped bool,
) int64 {
	return completeTestRunAt(t, repository, sourceURL, payload, summary, time.Now().UTC(), wantSkipped)
}

func completeTestRunAt(
	t *testing.T,
	repository *AlertRepository,
	sourceURL string,
	payload string,
	summary realtime.FeedSummary,
	retrievedAt time.Time,
	wantSkipped bool,
) int64 {
	t.Helper()
	ctx := context.Background()
	runID, err := repository.StartAlertRun(ctx, sourceURL)
	if err != nil {
		t.Fatalf("StartAlertRun() error = %v", err)
	}
	return completeExistingTestRun(t, repository, runID, payload, summary, retrievedAt, wantSkipped)
}

func completeExistingTestRun(
	t *testing.T,
	repository *AlertRepository,
	runID int64,
	payload string,
	summary realtime.FeedSummary,
	retrievedAt time.Time,
	wantSkipped bool,
) int64 {
	t.Helper()
	result := realtime.FetchResult{
		Body:        []byte(payload),
		StatusCode:  200,
		ContentType: "application/octet-stream",
		RetrievedAt: retrievedAt,
	}
	skipped, err := repository.CompleteAlertRun(context.Background(), runID, result, summary)
	if err != nil {
		t.Fatalf("CompleteAlertRun() error = %v", err)
	}
	if skipped != wantSkipped {
		t.Fatalf("CompleteAlertRun() skipped = %t, want %t", skipped, wantSkipped)
	}
	return runID
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Errorf("%s count = %d, want %d", table, got, want)
	}
}
