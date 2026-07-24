package database

import (
	"context"
	"database/sql"
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
	if err := repository.CompleteAlertRun(ctx, runID, result, summary); err != nil {
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
	if err := repository.CompleteAlertRun(ctx, runID, result, summary); err == nil {
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
	if _, err := db.Exec(`TRUNCATE ingestion_runs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset integration database: %v", err)
	}
	return db
}

func testSummary() realtime.FeedSummary {
	cause := "CONSTRUCTION"
	effect := "MODIFIED_SERVICE"
	direction := uint32(0)
	return realtime.FeedSummary{
		Timestamp:   &realtime.Timestamp{Unix: 1784883600, UTC: "2026-07-24T09:00:00Z"},
		EntityCount: 1,
		AlertCount:  1,
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
