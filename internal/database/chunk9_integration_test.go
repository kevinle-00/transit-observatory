package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/database/migrations"
	"github.com/kevinle-00/transit-observatory/internal/observability"
	"github.com/kevinle-00/transit-observatory/internal/realtime"
	"github.com/kevinle-00/transit-observatory/internal/storage"
	"github.com/pressly/goose/v3"
)

func TestArchivePersistenceIsIdempotentAndRejectsConflictingObject(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	object := storage.Object{
		Backend: "s3", Key: "alerts/shared", Size: 7,
		SHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StoredAt: time.Now().UTC(), ETag: "etag", VersionID: "version", Created: storage.Created(true),
	}
	if err := repository.RecordAlertArchive(ctx, runID, object); err != nil {
		t.Fatalf("RecordAlertArchive() error = %v", err)
	}
	retry := object
	retry.Created = storage.Created(false)
	if err := repository.RecordAlertArchive(ctx, runID, retry); err != nil {
		t.Fatalf("idempotent RecordAlertArchive() error = %v", err)
	}
	secondID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	conflict := object
	conflict.Size++
	if err := repository.RecordAlertArchive(ctx, secondID, conflict); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("conflicting RecordAlertArchive() error = %v, want storage.ErrConflict", err)
	}
	var status string
	var linked, created bool
	if err := db.QueryRow(`SELECT archive_status, raw_archive_id IS NOT NULL, archive_created FROM ingestion_runs WHERE id = $1`, runID).Scan(&status, &linked, &created); err != nil {
		t.Fatal(err)
	}
	if status != "archived" || !linked || !created {
		t.Fatalf("archive linkage = %q/%t/%t", status, linked, created)
	}
	versionConflictID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	versionConflict := object
	versionConflict.VersionID = "other-version"
	if err := repository.RecordAlertArchive(ctx, versionConflictID, versionConflict); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("version conflict error = %v, want storage.ErrConflict", err)
	}
	presentToEmptyID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	presentToEmpty := object
	presentToEmpty.VersionID = ""
	if err := repository.RecordAlertArchive(ctx, presentToEmptyID, presentToEmpty); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("present-to-empty version error = %v, want storage.ErrConflict", err)
	}
	unversioned := object
	unversioned.Key = "alerts/unversioned"
	unversioned.VersionID = ""
	unversionedID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordAlertArchive(ctx, unversionedID, unversioned); err != nil {
		t.Fatalf("record unversioned archive: %v", err)
	}
	emptyToPresentID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	emptyToPresent := unversioned
	emptyToPresent.VersionID = "new-version"
	if err := repository.RecordAlertArchive(ctx, emptyToPresentID, emptyToPresent); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("empty-to-present version error = %v, want storage.ErrConflict", err)
	}
}

func TestStatusPreservesAmbiguousArchiveCreation(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("ambiguous")
	digest := sha256.Sum256(payload)
	object := storage.Object{Backend: "s3", Key: "alerts/ambiguous", Size: int64(len(payload)),
		SHA256: hex.EncodeToString(digest[:]), StoredAt: now}
	if err := repository.RecordAlertArchive(ctx, runID, object); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteAlertRun(ctx, runID, realtime.FetchResult{Body: payload, RetrievedAt: now}, testSummaryAt(now)); err != nil {
		t.Fatal(err)
	}
	status, err := NewReadRepository(db).GetStatus(ctx, testStatusQuery(now))
	if err != nil {
		t.Fatal(err)
	}
	archive := status.ServiceAlerts.LastApplied.Archive
	if archive == nil || archive.ObjectKey != object.Key || archive.Created != nil {
		t.Fatalf("archive status = %#v", archive)
	}
}

func TestArchiveCommitResolutionDistinguishesLinkedAndPending(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	linkedID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	object := storage.Object{Backend: "test", Key: "alerts/resolution", Size: 1,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", StoredAt: time.Now().UTC()}
	if err := repository.RecordAlertArchive(ctx, linkedID, object); err != nil {
		t.Fatal(err)
	}
	committed, err := resolveArchiveCommit(ctx, db, "ingestion_runs", linkedID, object)
	if err != nil || !committed {
		t.Fatalf("resolve linked archive = %t, %v", committed, err)
	}
	pendingID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	committed, err = resolveArchiveCommit(ctx, db, "ingestion_runs", pendingID, object)
	if err != nil || committed {
		t.Fatalf("resolve pending archive = %t, %v", committed, err)
	}
	unknown := archiveCommitOutcomeError{commitError: errors.New("commit"), resolutionError: errors.New("query")}
	var marker interface{ CommitOutcomeUnknown() bool }
	if !errors.As(unknown, &marker) || !marker.CommitOutcomeUnknown() {
		t.Fatalf("archive commit error lacks unknown marker: %v", unknown)
	}
}

func TestPendingCompletionIsBlockedAndArchivedFailureRetainsArchive(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	result := realtime.FetchResult{Body: []byte("payload"), StatusCode: 200, RetrievedAt: time.Now().UTC()}
	summary := testSummaryAt(result.RetrievedAt)
	pendingID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteAlertRun(ctx, pendingID, result, summary); err == nil {
		t.Fatal("CompleteAlertRun() allowed pending archive")
	}
	var status, archiveStatus string
	if err := db.QueryRow(`SELECT status, archive_status FROM ingestion_runs WHERE id = $1`, pendingID).Scan(&status, &archiveStatus); err != nil {
		t.Fatal(err)
	}
	if status != "running" || archiveStatus != "pending" {
		t.Fatalf("pending attempt mutated to %s/%s", status, archiveStatus)
	}

	recordTestAlertArchive(t, repository, pendingID, result.Body)
	secret := errors.New("postgres password=do-not-return")
	if err := repository.FailAlertRunWithFailure(ctx, pendingID, &result, &summary, observability.Failure{
		Stage: "persist", Code: "write_failed", PublicMessage: "Unable to apply service alerts", Err: secret,
	}); err != nil {
		t.Fatal(err)
	}
	var archiveID *int64
	if err := db.QueryRow(`SELECT status, archive_status, raw_archive_id FROM ingestion_runs WHERE id = $1`, pendingID).Scan(
		&status, &archiveStatus, &archiveID); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || archiveStatus != "archived" || archiveID == nil {
		t.Fatalf("failed archived attempt = %s/%s/%v", status, archiveStatus, archiveID)
	}
}

func TestArchiveFailureDoesNotMutateCurrentAlerts(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	completeTestRunAt(t, repository, "https://example.test/alerts", "current", testSummaryAt(now), now, false)
	runID, err := repository.StartAlertRun(context.Background(), "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.FailAlertRunWithFailure(context.Background(), runID, nil, nil, observability.Failure{
		Stage: "archive", Code: "store_failed", PublicMessage: "Unable to archive feed", Err: errors.New("bucket secret"),
	}); err != nil {
		t.Fatal(err)
	}
	var present int
	if err := db.QueryRow(`SELECT count(*) FROM service_alerts WHERE is_present`).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 1 {
		t.Fatalf("present alerts = %d, want 1", present)
	}
}

func TestGetStatusEmptyFreshAndOperationalDegradation(t *testing.T) {
	db := integrationDatabase(t)
	reader := NewReadRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	query := testStatusQuery(now)
	empty, err := reader.GetStatus(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if empty.OverallStatus != OverallUnavailable || empty.ServiceAlerts.Freshness != "unavailable" ||
		empty.StaticGTFS.Freshness != "unavailable" || empty.ServiceAlerts.RecentFailures == nil {
		t.Fatalf("empty status = %#v", empty)
	}

	alerts := NewAlertRepository(db)
	alertSummary := testSummaryAt(now.Add(-time.Minute))
	alertSummary.Alerts[0].ActivePeriods = []realtime.ActivePeriod{{
		Start: &realtime.Timestamp{Unix: uint64(now.Add(-time.Hour).Unix())},
		End:   &realtime.Timestamp{Unix: uint64(now.Add(time.Hour).Unix())},
	}}
	completeTestRunAt(t, alerts, "https://example.test/alerts", "fresh", alertSummary, now.Add(-time.Minute), false)
	gtfsRepository := NewGTFSRepository(db)
	download := testGTFSDownloadAt("fresh", now.Add(-time.Hour))
	importID, err := gtfsRepository.StartImport(context.Background(), download.SourceURL)
	if err != nil {
		t.Fatal(err)
	}
	recordTestGTFSArchive(t, gtfsRepository, importID, download)
	if _, err := gtfsRepository.CompleteImport(context.Background(), importID, download, testGTFSDataset()); err != nil {
		t.Fatal(err)
	}
	fresh, err := reader.GetStatus(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.OverallStatus != OverallOK || fresh.ServiceAlerts.Freshness != "fresh" || fresh.StaticGTFS.Freshness != "fresh" {
		t.Fatalf("fresh status = %#v", fresh)
	}
	if fresh.ServiceAlerts.Counts.Present != 1 || fresh.ServiceAlerts.Counts.Current != 1 ||
		fresh.StaticGTFS.Counts.Routes != 2 || fresh.StaticGTFS.Counts.Stations != 2 || fresh.StaticGTFS.Counts.Relations != 2 {
		t.Fatalf("status counts = alerts %#v, GTFS %#v", fresh.ServiceAlerts.Counts, fresh.StaticGTFS.Counts)
	}

	failureID, err := alerts.StartAlertRun(context.Background(), "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2 WHERE id = $1`, failureID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := alerts.FailAlertRunWithFailure(context.Background(), failureID, nil, nil, observability.Failure{
		Stage: "fetch", Code: "upstream_unavailable", PublicMessage: "Alert source unavailable",
		Err: errors.New("endpoint=https://private.example token=secret"),
	}); err != nil {
		t.Fatal(err)
	}
	degraded, err := reader.GetStatus(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if degraded.OverallStatus != OverallDegraded || !containsString(degraded.ServiceAlerts.Reasons, "recent_failure") {
		t.Fatalf("degraded status = %#v", degraded)
	}
	encoded, err := json.Marshal(degraded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private.example") || strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "s3") {
		t.Fatalf("status leaked internal details: %s", encoded)
	}
}

func TestGetStatusFutureTimestampIsUnknownAndRunningCanBeOverdue(t *testing.T) {
	db := integrationDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	repository := NewAlertRepository(db)
	appliedID := completeTestRunAt(t, repository, "https://example.test/alerts", "future", testSummaryAt(now.Add(time.Hour)), now.Add(-2*time.Hour), false)
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2, completed_at = $3 WHERE id = $1`,
		appliedID, now.Add(-3*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	runID, err := repository.StartAlertRun(context.Background(), "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2 WHERE id = $1`, runID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	status, err := NewReadRepository(db).GetStatus(context.Background(), testStatusQuery(now))
	if err != nil {
		t.Fatal(err)
	}
	if status.ServiceAlerts.Freshness != "unknown" || status.ServiceAlerts.DataAgeSeconds != nil ||
		!containsString(status.ServiceAlerts.Reasons, "data_timestamp_in_future") ||
		!containsString(status.ServiceAlerts.Reasons, "run_overdue") || status.ServiceAlerts.LatestAttempt == nil || !status.ServiceAlerts.LatestAttempt.Overdue {
		t.Fatalf("future/overdue status = %#v", status.ServiceAlerts)
	}
}

func TestStatusOperationalFailuresUseCompletionOrderNotRetrievalTime(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	appliedID := completeTestRunAt(t, repository, "https://example.test/alerts", "first", testSummaryAt(base), base, false)
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2, completed_at = $3, retrieved_at = $4 WHERE id = $1`,
		appliedID, base.Add(-time.Minute), base, base.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	failureID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.FailAlertRunWithFailure(ctx, failureID, nil, nil, observability.Failure{
		Stage: "fetch", Code: "fetch_failed", PublicMessage: "Fetch failed", Err: errors.New("private"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2, completed_at = $3 WHERE id = $1`,
		failureID, base.Add(time.Minute), base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	query := testStatusQuery(base.Add(4 * time.Minute))
	status, err := NewReadRepository(db).GetStatus(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(status.ServiceAlerts.Reasons, "recent_failure") {
		t.Fatalf("future retrieval suppressed later failure: %#v", status.ServiceAlerts)
	}

	successID := completeTestRunAt(t, repository, "https://example.test/alerts", "second", testSummaryAt(base.Add(time.Minute)), base.Add(time.Minute), false)
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2, completed_at = $3 WHERE id = $1`,
		successID, base.Add(2*time.Minute), base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	status, err = NewReadRepository(db).GetStatus(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(status.ServiceAlerts.Reasons, "recent_failure") {
		t.Fatalf("later success did not supersede failure: %#v", status.ServiceAlerts)
	}
}

func TestAcceptedCheckSeparatesLatestRetrievalFromOperationalCompletion(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewAlertRepository(db)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	newerRetrievalID := completeTestRunAt(t, repository, "https://example.test/alerts", "newer-retrieval",
		testSummaryAt(base), base.Add(2*time.Minute), false)
	earlierRetrievalID := completeTestRunAt(t, repository, "https://example.test/alerts", "earlier-retrieval",
		testSummaryAt(base.Add(time.Minute)), base.Add(time.Minute), false)
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2, completed_at = $3 WHERE id = $1`,
		newerRetrievalID, base, base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2, completed_at = $3 WHERE id = $1`,
		earlierRetrievalID, base.Add(time.Minute), base.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	check, err := acceptedAlertCheck(ctx, tx)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if check == nil || check.retrievedAt == nil || !check.retrievedAt.Equal(base.Add(2*time.Minute)) ||
		check.id != earlierRetrievalID || !check.completedAt.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("accepted check = %#v", check)
	}

	failureID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.FailAlertRunWithFailure(ctx, failureID, nil, nil, observability.Failure{
		Stage: "fetch", Code: "fetch_failed", PublicMessage: "Fetch failed", Err: errors.New("private"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE ingestion_runs SET started_at = $2, completed_at = $3 WHERE id = $1`,
		failureID, base.Add(2*time.Minute), base.Add(3*time.Minute+30*time.Second)); err != nil {
		t.Fatal(err)
	}
	status, err := NewReadRepository(db).GetStatus(ctx, testStatusQuery(base.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if status.ServiceAlerts.CheckAt == nil || !status.ServiceAlerts.CheckAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("CheckAt = %v, want latest retrieval", status.ServiceAlerts.CheckAt)
	}
	if containsString(status.ServiceAlerts.Reasons, "recent_failure") {
		t.Fatalf("later accepted completion did not supersede failure: %#v", status.ServiceAlerts)
	}
}

func testStatusQuery(now time.Time) StatusQuery {
	return StatusQuery{Now: now, AlertDataMaxAge: 10 * time.Minute, AlertCheckMaxAge: 10 * time.Minute,
		GTFSDataMaxAge: 192 * time.Hour, GTFSCheckMaxAge: 36 * time.Hour,
		AlertRunMaxDuration: 5 * time.Minute, GTFSRunMaxDuration: 30 * time.Minute,
		FutureTolerance: 2 * time.Minute, RecentFailureLimit: 5}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestArchivalMigrationDownAndUp(t *testing.T) {
	db := integrationDatabase(t)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(context.Background()); err != nil {
		t.Fatalf("migration Down() error = %v", err)
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'raw_archives')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("raw_archives still exists after migration down")
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatalf("migration Up() error = %v", err)
	}
}
