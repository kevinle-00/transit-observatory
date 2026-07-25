package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/gtfs"
	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

func TestGTFSRepositoryImportsAndSkipsDuplicate(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewGTFSRepository(db)
	ctx := context.Background()
	download := testGTFSDownload("hash-one")
	dataset := testGTFSDataset()

	importID, err := repository.StartImport(ctx, download.SourceURL)
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	skipped, err := repository.CompleteImport(ctx, importID, download, dataset)
	if err != nil {
		t.Fatalf("CompleteImport() error = %v", err)
	}
	if skipped {
		t.Error("first import was skipped")
	}
	assertCount(t, db, "routes", 2)
	assertCount(t, db, "stops", 3)
	assertCount(t, db, "route_stations", 2)

	duplicateID, err := repository.StartImport(ctx, download.SourceURL)
	if err != nil {
		t.Fatalf("StartImport() duplicate error = %v", err)
	}
	skipped, err = repository.SkipIfImported(ctx, duplicateID, download)
	if err != nil {
		t.Fatalf("SkipIfImported() error = %v", err)
	}
	if !skipped {
		t.Error("duplicate import was not skipped")
	}
	var status string
	if err := db.QueryRow("SELECT status FROM gtfs_imports WHERE id = $1", duplicateID).Scan(&status); err != nil {
		t.Fatalf("query duplicate import: %v", err)
	}
	if status != "skipped" {
		t.Errorf("duplicate status = %q, want skipped", status)
	}
	currentSummary, err := repository.CurrentSummary(ctx)
	if err != nil {
		t.Fatalf("CurrentSummary() error = %v", err)
	}
	if currentSummary.RouteCount != dataset.Summary.RouteCount || currentSummary.StopCount != dataset.Summary.StopCount {
		t.Errorf("current summary = %#v, want %#v", currentSummary, dataset.Summary)
	}
}

func TestGTFSRepositoryRollbackPreservesCurrentNetwork(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewGTFSRepository(db)
	ctx := context.Background()

	firstID, err := repository.StartImport(ctx, "https://example.test/gtfs.zip")
	if err != nil {
		t.Fatalf("StartImport() first error = %v", err)
	}
	if _, err := repository.CompleteImport(ctx, firstID, testGTFSDownload("first-hash"), testGTFSDataset()); err != nil {
		t.Fatalf("CompleteImport() first error = %v", err)
	}

	invalid := testGTFSDataset()
	invalid.RouteStations = append(invalid.RouteStations, gtfs.RouteStation{RouteID: "missing", StationID: "station-a"})
	secondDownload := testGTFSDownload("second-hash")
	secondID, err := repository.StartImport(ctx, secondDownload.SourceURL)
	if err != nil {
		t.Fatalf("StartImport() second error = %v", err)
	}
	if _, err := repository.CompleteImport(ctx, secondID, secondDownload, invalid); err == nil {
		t.Fatal("CompleteImport() invalid error = nil")
	}
	if err := repository.FailImport(ctx, secondID, &secondDownload, context.Canceled); err != nil {
		t.Fatalf("FailImport() error = %v", err)
	}

	assertCount(t, db, "routes", 2)
	assertCount(t, db, "stops", 3)
	assertCount(t, db, "route_stations", 2)
	var currentImportID int64
	if err := db.QueryRow("SELECT DISTINCT gtfs_import_id FROM routes").Scan(&currentImportID); err != nil {
		t.Fatalf("query current route import: %v", err)
	}
	if currentImportID != firstID {
		t.Errorf("current import ID = %d, want %d", currentImportID, firstID)
	}
}

func TestGTFSRepositoryReimportsHistoricalHashWhenItBecomesCurrentAgain(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewGTFSRepository(db)
	ctx := context.Background()
	base := time.Date(2026, time.July, 25, 1, 0, 0, 0, time.UTC)

	first := testGTFSDownloadAt("hash-a", base)
	firstID, err := repository.StartImport(ctx, first.SourceURL)
	if err != nil {
		t.Fatalf("start first A import: %v", err)
	}
	if _, err := repository.CompleteImport(ctx, firstID, first, testGTFSDataset()); err != nil {
		t.Fatalf("complete first A import: %v", err)
	}
	second := testGTFSDownloadAt("hash-b", base.Add(time.Hour))
	secondID, err := repository.StartImport(ctx, second.SourceURL)
	if err != nil {
		t.Fatalf("start B import: %v", err)
	}
	if _, err := repository.CompleteImport(ctx, secondID, second, testGTFSDataset()); err != nil {
		t.Fatalf("complete B import: %v", err)
	}
	third := testGTFSDownloadAt("hash-a", base.Add(2*time.Hour))
	thirdID, err := repository.StartImport(ctx, third.SourceURL)
	if err != nil {
		t.Fatalf("start second A import: %v", err)
	}
	skipped, err := repository.CompleteImport(ctx, thirdID, third, testGTFSDataset())
	if err != nil {
		t.Fatalf("complete second A import: %v", err)
	}
	if skipped {
		t.Error("historical hash A was skipped even though B was current")
	}
	var currentID int64
	if err := db.QueryRow("SELECT id FROM gtfs_imports WHERE is_current").Scan(&currentID); err != nil {
		t.Fatalf("query current import: %v", err)
	}
	if currentID != thirdID {
		t.Errorf("current import = %d, want %d", currentID, thirdID)
	}
}

func TestGTFSRepositoryDoesNotReplaceNewerNetworkWithOlderDownload(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewGTFSRepository(db)
	ctx := context.Background()
	base := time.Date(2026, time.July, 25, 2, 0, 0, 0, time.UTC)

	olderID, err := repository.StartImport(ctx, "https://example.test/gtfs.zip")
	if err != nil {
		t.Fatalf("start older import: %v", err)
	}
	newerID, err := repository.StartImport(ctx, "https://example.test/gtfs.zip")
	if err != nil {
		t.Fatalf("start newer import: %v", err)
	}
	newer := testGTFSDownloadAt("newer", base.Add(time.Hour))
	if _, err := repository.CompleteImport(ctx, newerID, newer, testGTFSDataset()); err != nil {
		t.Fatalf("complete newer import: %v", err)
	}
	older := testGTFSDownloadAt("older", base)
	skipped, err := repository.CompleteImport(ctx, olderID, older, testGTFSDataset())
	if err != nil {
		t.Fatalf("complete older import: %v", err)
	}
	if !skipped {
		t.Error("older import was allowed to replace newer network")
	}
	var currentID int64
	if err := db.QueryRow("SELECT id FROM gtfs_imports WHERE is_current").Scan(&currentID); err != nil {
		t.Fatalf("query current import: %v", err)
	}
	if currentID != newerID {
		t.Errorf("current import = %d, want newer %d", currentID, newerID)
	}
}

func TestGTFSRepositoryUsesRequestTimeWhenSourceTimestampTies(t *testing.T) {
	db := integrationDatabase(t)
	repository := NewGTFSRepository(db)
	ctx := context.Background()
	base := time.Date(2026, time.July, 25, 3, 0, 0, 0, time.UTC)

	current := testGTFSDownloadAt("current", base)
	current.RequestedAt = base.Add(2 * time.Minute)
	current.RetrievedAt = base.Add(3 * time.Minute)
	currentID, err := repository.StartImport(ctx, current.SourceURL)
	if err != nil {
		t.Fatalf("start current import: %v", err)
	}
	if _, err := repository.CompleteImport(ctx, currentID, current, testGTFSDataset()); err != nil {
		t.Fatalf("complete current import: %v", err)
	}

	stale := testGTFSDownloadAt("stale", base)
	stale.RequestedAt = base.Add(time.Minute)
	stale.RetrievedAt = base.Add(4 * time.Minute)
	staleID, err := repository.StartImport(ctx, stale.SourceURL)
	if err != nil {
		t.Fatalf("start stale import: %v", err)
	}
	skipped, err := repository.CompleteImport(ctx, staleID, stale, testGTFSDataset())
	if err != nil {
		t.Fatalf("complete stale import: %v", err)
	}
	if !skipped {
		t.Error("slow stale response with equal source timestamp was applied")
	}
}

func TestGTFSRepositoryMeasuresRealtimeIdentifierCoverage(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	gtfsRepository := NewGTFSRepository(db)
	importID, err := gtfsRepository.StartImport(ctx, "https://example.test/gtfs.zip")
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	dataset := testGTFSDataset()
	dataset.Routes[0].ID = "aus:vic:vic-02-FKN:"
	dataset.RouteStations[0].RouteID = "aus:vic:vic-02-FKN:"
	dataset.Stops[0].ID = "vic:rail:ARM"
	dataset.Stops[2].ParentStationID = "vic:rail:ARM"
	dataset.RouteStations[0].StationID = "vic:rail:ARM"
	if _, err := gtfsRepository.CompleteImport(ctx, importID, testGTFSDownload("coverage-hash"), dataset); err != nil {
		t.Fatalf("CompleteImport() error = %v", err)
	}

	alertRepository := NewAlertRepository(db)
	alertRunID, err := alertRepository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() error = %v", err)
	}
	summary := testSummary()
	summary.Alerts[0].InformedEntities[0].TripRouteID = summary.Alerts[0].InformedEntities[0].RouteID
	summary.Alerts[0].InformedEntities[0].RouteID = ""
	if _, err := alertRepository.CompleteAlertRun(ctx, alertRunID, realtime.FetchResult{
		Body: []byte("coverage-alert"), StatusCode: 200, RetrievedAt: time.Now().UTC(),
	}, summary); err != nil {
		t.Fatalf("CompleteAlertRun() error = %v", err)
	}

	coverage, err := gtfsRepository.Coverage(ctx)
	if err != nil {
		t.Fatalf("Coverage() error = %v", err)
	}
	if coverage.RealtimeRouteCount != 1 || coverage.MatchedRouteCount != 1 ||
		coverage.RealtimeStopCount != 1 || coverage.MatchedStopCount != 1 {
		t.Errorf("coverage = %#v", coverage)
	}
}

func testGTFSDownload(hash string) gtfs.Download {
	return testGTFSDownloadAt(hash, time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC))
}

func testGTFSDownloadAt(hash string, modifiedAt time.Time) gtfs.Download {
	modifiedAt = modifiedAt.UTC()
	digest := sha256.Sum256([]byte(hash))
	return gtfs.Download{
		SourceURL:    "https://example.test/gtfs.zip",
		RequestedAt:  modifiedAt.Add(30 * time.Second),
		RetrievedAt:  modifiedAt.Add(time.Minute),
		SHA256:       hex.EncodeToString(digest[:]),
		Size:         1024,
		ETag:         `"fixture"`,
		LastModified: "Sat, 25 Jul 2026 00:00:00 GMT",
		ModifiedAt:   &modifiedAt,
	}
}

func testGTFSDataset() gtfs.Dataset {
	wheelchair := 1
	latitudeA, longitudeA := -37.8, 144.9
	latitudeB, longitudeB := -37.9, 145.0
	return gtfs.Dataset{
		Routes: []gtfs.Route{
			{ID: "route-a", ShortName: "Line A", LongName: "Line A - City", Type: 400, Color: "112233", TextColor: "FFFFFF"},
			{ID: "route-b", ShortName: "Replacement Bus", LongName: "Line B", Type: 400, IsReplacementBus: true},
		},
		Stops: []gtfs.Stop{
			{ID: "station-a", Name: "Station A", Latitude: &latitudeA, Longitude: &longitudeA, LocationType: 1, WheelchairBoarding: &wheelchair},
			{ID: "station-b", Name: "Station B", Latitude: &latitudeB, Longitude: &longitudeB, LocationType: 1},
			{ID: "platform-a", Name: "Station A", Latitude: &latitudeA, Longitude: &longitudeA, ParentStationID: "station-a", PlatformCode: "1"},
		},
		RouteStations: []gtfs.RouteStation{
			{RouteID: "route-a", StationID: "station-a"},
			{RouteID: "route-b", StationID: "station-b"},
		},
		Summary: gtfs.Summary{
			RouteCount: 2, StopCount: 3, StationCount: 2, TripCount: 2,
			StopTimeCount: 4, RouteStationCount: 2, MetroArchiveBytes: 512,
		},
	}
}
