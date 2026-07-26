package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/gtfs"
	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

func TestCurrentAlertReaderEnrichesAndPreservesIdentifiers(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	installEnrichmentNetwork(t, ctx, db)
	installEnrichmentAlert(t, ctx, db)

	alerts, err := NewCurrentAlertReader(db).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("alert count = %d, want 1", len(alerts))
	}
	alert := alerts[0]
	if alert.SourceEntityID != "enrichment-alert" || alert.RevisionNumber != 1 {
		t.Errorf("alert identity = %#v", alert)
	}
	if len(alert.Header) != 1 || alert.Header[0].Text != "Network disruption" {
		t.Errorf("header = %#v", alert.Header)
	}
	if len(alert.ActivePeriods) != 1 || alert.ActivePeriods[0].StartsAt == nil {
		t.Errorf("active periods = %#v", alert.ActivePeriods)
	}

	routes := make(map[string]CurrentAlertRoute)
	for _, route := range alert.Routes {
		routes[route.SourceRouteID] = route
	}
	if len(routes) != 3 {
		t.Fatalf("route count = %d, want 3: %#v", len(routes), routes)
	}
	if !routes["route-direct"].IsMatched || routes["route-direct"].ShortName == nil || *routes["route-direct"].ShortName != "Direct Line" {
		t.Errorf("direct route = %#v", routes["route-direct"])
	}
	if !routes["route-trip"].IsMatched || routes["route-trip"].StaticRouteID == nil {
		t.Errorf("trip route = %#v", routes["route-trip"])
	}
	if routes["route-missing"].IsMatched || routes["route-missing"].StaticRouteID != nil {
		t.Errorf("unmatched route = %#v", routes["route-missing"])
	}

	stations := make(map[string]CurrentAlertStation)
	for _, station := range alert.Stations {
		stations[station.SourceStopID] = station
	}
	if len(stations) != 3 {
		t.Fatalf("station count = %d, want 3: %#v", len(stations), stations)
	}
	for _, sourceID := range []string{"station-a", "boarding-a"} {
		station := stations[sourceID]
		if !station.IsMatched || station.StaticStationID == nil || *station.StaticStationID != "station-a" {
			t.Errorf("resolved station %q = %#v", sourceID, station)
		}
	}
	if stations["stop-missing"].IsMatched || stations["stop-missing"].StaticStationID != nil {
		t.Errorf("unmatched station = %#v", stations["stop-missing"])
	}
}

func TestCurrentAlertReaderExcludesClosedAlerts(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	installEnrichmentNetwork(t, ctx, db)
	installEnrichmentAlert(t, ctx, db)

	repository := NewAlertRepository(db)
	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() close error = %v", err)
	}
	closedAt := time.Date(2026, time.July, 25, 6, 5, 0, 0, time.UTC)
	closeResult := realtime.FetchResult{
		Body: []byte("empty-full-feed"), StatusCode: 200, RetrievedAt: closedAt,
	}
	recordTestAlertArchive(t, repository, runID, closeResult.Body)
	if _, err := repository.CompleteAlertRun(ctx, runID, closeResult, realtime.FeedSummary{
		Incrementality: "FULL_DATASET",
		Timestamp:      &realtime.Timestamp{Unix: uint64(closedAt.Unix())},
		Alerts:         []realtime.AlertSummary{},
	}); err != nil {
		t.Fatalf("CompleteAlertRun() close error = %v", err)
	}

	alerts, err := NewCurrentAlertReader(db).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("closed alert count = %d, want 0", len(alerts))
	}
}

func TestCurrentAlertReaderExcludesDeletedRevisions(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	repository := NewAlertRepository(db)
	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() error = %v", err)
	}
	observedAt := time.Date(2026, time.July, 25, 6, 10, 0, 0, time.UTC)
	deletedResult := realtime.FetchResult{
		Body: []byte("deleted-alert"), StatusCode: 200, RetrievedAt: observedAt,
	}
	recordTestAlertArchive(t, repository, runID, deletedResult.Body)
	if _, err := repository.CompleteAlertRun(ctx, runID, deletedResult, realtime.FeedSummary{
		Incrementality: "FULL_DATASET",
		Timestamp:      &realtime.Timestamp{Unix: uint64(observedAt.Unix())},
		EntityCount:    1,
		AlertCount:     1,
		Alerts: []realtime.AlertSummary{
			{EntityID: "deleted-alert", Deleted: true},
		},
	}); err != nil {
		t.Fatalf("CompleteAlertRun() error = %v", err)
	}
	alerts, err := NewCurrentAlertReader(db).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("deleted current alert count = %d, want 0", len(alerts))
	}
}

func installEnrichmentNetwork(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	repository := NewGTFSRepository(db)
	importID, err := repository.StartImport(ctx, "https://example.test/gtfs.zip")
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	latitude, longitude := -37.8, 144.9
	modifiedAt := time.Date(2026, time.July, 25, 6, 0, 0, 0, time.UTC)
	dataset := gtfs.Dataset{
		Routes: []gtfs.Route{
			{ID: "route-direct", ShortName: "Direct Line", LongName: "Direct - City", Type: 400, Color: "112233", TextColor: "FFFFFF"},
			{ID: "route-trip", ShortName: "Trip Line", LongName: "Trip - City", Type: 400, Color: "445566", TextColor: "FFFFFF"},
		},
		Stops: []gtfs.Stop{
			{ID: "station-a", Name: "Station A", Latitude: &latitude, Longitude: &longitude, LocationType: 1},
			{ID: "platform-a", Name: "Station A", Latitude: &latitude, Longitude: &longitude, LocationType: 0, ParentStationID: "station-a"},
			{ID: "boarding-a", LocationType: 4, ParentStationID: "platform-a"},
		},
		RouteStations: []gtfs.RouteStation{
			{RouteID: "route-direct", StationID: "station-a"},
			{RouteID: "route-trip", StationID: "station-a"},
		},
		Summary: gtfs.Summary{RouteCount: 2, StopCount: 3, StationCount: 1, TripCount: 2, StopTimeCount: 2, RouteStationCount: 2},
	}
	download := gtfs.Download{
		SourceURL: "https://example.test/gtfs.zip", RequestedAt: modifiedAt,
		RetrievedAt: modifiedAt.Add(time.Minute), ModifiedAt: &modifiedAt,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 100,
	}
	recordTestGTFSArchive(t, repository, importID, download)
	if _, err := repository.CompleteImport(ctx, importID, download, dataset); err != nil {
		t.Fatalf("CompleteImport() error = %v", err)
	}
}

func installEnrichmentAlert(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	repository := NewAlertRepository(db)
	runID, err := repository.StartAlertRun(ctx, "https://example.test/alerts")
	if err != nil {
		t.Fatalf("StartAlertRun() error = %v", err)
	}
	observedAt := time.Date(2026, time.July, 25, 6, 1, 0, 0, time.UTC)
	direction := uint32(0)
	summary := realtime.FeedSummary{
		Incrementality: "FULL_DATASET",
		Timestamp:      &realtime.Timestamp{Unix: uint64(observedAt.Unix())},
		EntityCount:    1,
		AlertCount:     1,
		Alerts: []realtime.AlertSummary{
			{
				EntityID:    "enrichment-alert",
				Header:      []realtime.Translation{{Text: "Network disruption", Language: "en"}},
				Description: []realtime.Translation{{Text: "Allow extra time", Language: "en"}},
				ActivePeriods: []realtime.ActivePeriod{
					{Start: &realtime.Timestamp{Unix: uint64(observedAt.Unix())}},
				},
				InformedEntities: []realtime.InformedEntity{
					{RouteID: "route-direct", TripRouteID: "route-direct", StopID: "station-a", DirectionID: &direction},
					{TripRouteID: "route-trip", StopID: "boarding-a"},
					{RouteID: "route-missing", StopID: "stop-missing"},
				},
			},
		},
	}
	alertResult := realtime.FetchResult{
		Body: []byte("enrichment-feed"), StatusCode: 200, RetrievedAt: observedAt,
	}
	recordTestAlertArchive(t, repository, runID, alertResult.Body)
	if _, err := repository.CompleteAlertRun(ctx, runID, alertResult, summary); err != nil {
		t.Fatalf("CompleteAlertRun() error = %v", err)
	}
}
