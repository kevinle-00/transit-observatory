package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

func TestReadRepositoryAlertStatusesFiltersAndPagination(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	installEnrichmentNetwork(t, ctx, db)
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	cause := "CONSTRUCTION"
	effect := "MODIFIED_SERVICE"
	summary := realtime.FeedSummary{
		Incrementality: "FULL_DATASET",
		Timestamp:      &realtime.Timestamp{Unix: uint64(now.Add(-time.Minute).Unix())},
		EntityCount:    3,
		AlertCount:     3,
		Alerts: []realtime.AlertSummary{
			{
				EntityID: "boundary-current", Cause: &cause, Effect: &effect,
				ActivePeriods: []realtime.ActivePeriod{{
					Start: &realtime.Timestamp{Unix: uint64(now.Unix())},
					End:   &realtime.Timestamp{Unix: uint64(now.Add(time.Hour).Unix())},
				}},
				InformedEntities: []realtime.InformedEntity{{RouteID: "route-direct", TripRouteID: "route-direct"}},
			},
			{
				EntityID: "boundary-upcoming", Cause: &cause,
				ActivePeriods: []realtime.ActivePeriod{{
					Start: &realtime.Timestamp{Unix: uint64(now.Add(time.Hour).Unix())},
				}},
				InformedEntities: []realtime.InformedEntity{{TripRouteID: "route-trip"}},
			},
			{EntityID: "unbounded-current", InformedEntities: []realtime.InformedEntity{{StopID: "boarding-a"}}},
		},
	}
	repository := NewAlertRepository(db)
	completeTestRunAt(t, repository, "https://example.test/alerts", "status-feed", summary, now, false)
	reader := NewReadRepository(db)

	current, err := reader.ListAlerts(ctx, AlertQuery{Status: AlertStatusCurrent, Now: now})
	if err != nil {
		t.Fatalf("ListAlerts(current) error = %v", err)
	}
	if len(current.Alerts) != 2 {
		t.Fatalf("current alert count = %d, want 2", len(current.Alerts))
	}
	upcoming, err := reader.ListAlerts(ctx, AlertQuery{Status: AlertStatusUpcoming, Now: now, LineID: "route-trip"})
	if err != nil {
		t.Fatalf("ListAlerts(upcoming) error = %v", err)
	}
	if len(upcoming.Alerts) != 1 || upcoming.Alerts[0].SourceEntityID != "boundary-upcoming" {
		t.Errorf("upcoming alerts = %#v", upcoming.Alerts)
	}
	windowEnd := now
	beforeBoundary, err := reader.ListAlerts(ctx, AlertQuery{
		Status: AlertStatusPresent, Now: now, Cause: cause, To: &windowEnd,
	})
	if err != nil {
		t.Fatalf("ListAlerts(boundary) error = %v", err)
	}
	if len(beforeBoundary.Alerts) != 0 {
		t.Errorf("alerts before exclusive boundary = %d, want 0", len(beforeBoundary.Alerts))
	}
	beforeObservation := now.Add(-2 * time.Minute)
	noPeriodBeforeObservation, err := reader.ListAlerts(ctx, AlertQuery{
		Status: AlertStatusPresent, Now: now, To: &beforeObservation,
	})
	if err != nil {
		t.Fatalf("ListAlerts(no-period observation) error = %v", err)
	}
	if len(noPeriodBeforeObservation.Alerts) != 0 {
		t.Errorf("alerts before observation = %d, want 0", len(noPeriodBeforeObservation.Alerts))
	}

	empty := realtime.FeedSummary{Incrementality: "FULL_DATASET", Timestamp: &realtime.Timestamp{Unix: uint64(now.Add(2 * time.Hour).Unix())}}
	completeTestRunAt(t, repository, "https://example.test/alerts", "empty-status-feed", empty, now.Add(2*time.Hour), false)
	historical, err := reader.ListAlerts(ctx, AlertQuery{Status: AlertStatusHistorical, Now: now, Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("ListAlerts(historical) error = %v", err)
	}
	if len(historical.Alerts) != 1 || historical.Total != 3 || historical.Page != 2 {
		t.Errorf("historical page = %#v", historical)
	}
}

func TestReadRepositoryDetailsNetworkAndLiteralSearch(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	installEnrichmentNetwork(t, ctx, db)
	if _, err := db.Exec("UPDATE stops SET name = 'Station % A' WHERE stop_id IN ('station-a', 'platform-a')"); err != nil {
		t.Fatalf("rename fixture station: %v", err)
	}
	installEnrichmentAlert(t, ctx, db)
	reader := NewReadRepository(db)
	now := time.Date(2026, time.July, 25, 6, 2, 0, 0, time.UTC)

	lines, err := reader.ListLines(ctx, false, now)
	if err != nil {
		t.Fatalf("ListLines() error = %v", err)
	}
	if len(lines) != 2 || lines[0].StationCount != 1 || lines[0].PresentAlertCount != 1 {
		t.Errorf("lines = %#v", lines)
	}
	line, err := reader.GetLine(ctx, "route-trip", now)
	if err != nil {
		t.Fatalf("GetLine() error = %v", err)
	}
	if len(line.Stations) != 1 || len(line.Alerts) != 1 {
		t.Errorf("line detail = %#v", line)
	}
	stations, err := reader.ListStations(ctx, StationQuery{Q: "%", LineID: "route-direct"}, now)
	if err != nil {
		t.Fatalf("ListStations() error = %v", err)
	}
	if len(stations) != 1 || len(stations[0].Lines) != 2 || stations[0].PresentAlertCount != 1 {
		t.Errorf("stations = %#v", stations)
	}
	station, err := reader.GetStation(ctx, "station-a", now)
	if err != nil {
		t.Fatalf("GetStation() error = %v", err)
	}
	if len(station.Alerts) != 1 {
		t.Errorf("station alerts = %#v", station.Alerts)
	}
	alertID := station.Alerts[0].ID
	detail, err := reader.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatalf("GetAlert() error = %v", err)
	}
	if detail.Status != AlertStatusPresent || detail.RevisionCount != 1 || len(detail.LatestRevision.Routes) != 3 {
		t.Errorf("alert detail = %#v", detail)
	}
	if _, err := reader.GetLine(ctx, "missing", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLine(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := reader.GetStation(ctx, "missing", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetStation(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := reader.GetAlert(ctx, alertID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAlert(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := reader.ListAlertRevisions(ctx, alertID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListAlertRevisions(missing) error = %v, want ErrNotFound", err)
	}
}

func TestReadRepositoryRevisionTimelineAndEpisodeAnalytics(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	installEnrichmentNetwork(t, ctx, db)
	writer := NewAlertRepository(db)
	base := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	cause := "WEATHER"
	effect := "DETOUR"
	alert := realtime.AlertSummary{
		EntityID: "episode-alert", Cause: &cause, Effect: &effect,
		InformedEntities: []realtime.InformedEntity{{RouteID: "route-direct", TripRouteID: "route-direct", StopID: "platform-a"}},
	}
	feed := func(at time.Time, alerts []realtime.AlertSummary, payload string) {
		summary := realtime.FeedSummary{
			Incrementality: "FULL_DATASET", Timestamp: &realtime.Timestamp{Unix: uint64(at.Unix())},
			EntityCount: len(alerts), AlertCount: len(alerts), Alerts: alerts,
		}
		completeTestRunAt(t, writer, "https://example.test/alerts", payload, summary, at, false)
	}
	feed(base, []realtime.AlertSummary{alert}, "episode-1")
	alert.Header = []realtime.Translation{{Text: "changed"}}
	feed(base.Add(time.Minute), []realtime.AlertSummary{alert}, "episode-change")
	feed(base.Add(2*time.Minute), nil, "episode-gap")
	feed(base.Add(10*time.Minute), []realtime.AlertSummary{alert}, "episode-2")
	deleted := alert
	deleted.Deleted = true
	feed(base.Add(11*time.Minute), []realtime.AlertSummary{deleted}, "episode-delete")
	changedDeleted := deleted
	changedDeleted.Header = []realtime.Translation{{Text: "changed deletion"}}
	feed(base.Add(15*time.Minute), []realtime.AlertSummary{changedDeleted}, "episode-delete-change")
	feed(base.Add(20*time.Minute), []realtime.AlertSummary{changedDeleted}, "episode-delete-repeat")
	feed(base.Add(21*time.Minute), []realtime.AlertSummary{alert}, "episode-reappear")
	feed(base.Add(22*time.Minute), nil, "episode-close-reappearance")

	reader := NewReadRepository(db)
	var alertID int64
	if err := db.QueryRow("SELECT id FROM service_alerts WHERE source_entity_id = 'episode-alert'").Scan(&alertID); err != nil {
		t.Fatalf("query alert ID: %v", err)
	}
	revisions, err := reader.ListAlertRevisions(ctx, alertID)
	if err != nil {
		t.Fatalf("ListAlertRevisions() error = %v", err)
	}
	if len(revisions) != 6 || !revisions[3].IsDeleted || !revisions[4].IsDeleted || revisions[5].IsDeleted ||
		len(revisions[0].Routes) != 1 || len(revisions[0].Stations) != 1 {
		t.Errorf("revisions = %#v", revisions)
	}
	var episodeCount int
	var deletedEpisodeLastSeen time.Time
	if err := db.QueryRow(`
		SELECT count(*), max(last_seen_at) FILTER (WHERE episode_number = 2)
		FROM alert_episodes
		WHERE service_alert_id = $1`, alertID).Scan(&episodeCount, &deletedEpisodeLastSeen); err != nil {
		t.Fatalf("query episodes: %v", err)
	}
	if episodeCount != 3 || !deletedEpisodeLastSeen.Equal(base.Add(11*time.Minute)) {
		t.Errorf("episodes = %d, deleted episode last seen = %s", episodeCount, deletedEpisodeLastSeen)
	}
	analytics, err := reader.GetLineAnalytics(ctx, "route-direct", AnalyticsQuery{
		Now: base.Add(23 * time.Hour), From: base.Add(-time.Hour), To: base.Add(24 * time.Hour), Interval: "day",
	})
	if err != nil {
		t.Fatalf("GetLineAnalytics() error = %v", err)
	}
	if len(analytics.Series) != 2 || analytics.Series[0].AlertCount != 3 ||
		analytics.Series[0].CompletedEpisodeSampleCount != 3 ||
		analytics.Series[0].MedianObservedLifetimeSeconds == nil ||
		*analytics.Series[0].MedianObservedLifetimeSeconds != 60 {
		t.Errorf("analytics series = %#v", analytics.Series)
	}
	if len(analytics.Causes) != 1 || analytics.Causes[0].Value != cause || analytics.Causes[0].Count != 3 {
		t.Errorf("cause breakdown = %#v", analytics.Causes)
	}
	if len(analytics.MetricLimitations) == 0 {
		t.Error("detailed analytics metric limitations are empty")
	}
	collection, err := reader.ListLineAnalytics(ctx, AnalyticsQuery{
		Now: base.Add(23 * time.Hour), From: base.Add(-time.Hour), To: base.Add(24 * time.Hour), Interval: "day",
	})
	if err != nil {
		t.Fatalf("ListLineAnalytics() error = %v", err)
	}
	if len(collection) != 2 || len(collection[0].Series) != 2 || len(collection[1].Series) != 2 {
		t.Errorf("analytics collection = %#v", collection)
	}
	if _, err := reader.GetLineAnalytics(ctx, "missing", AnalyticsQuery{Now: base, From: base, To: base.Add(time.Hour), Interval: "hour"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLineAnalytics(missing) error = %v, want ErrNotFound", err)
	}
}

func TestReadRepositoryHistoricalFiltersSelectDisruptionRevisionBeforeSparseDeletion(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	installEnrichmentNetwork(t, ctx, db)
	writer := NewAlertRepository(db)
	reader := NewReadRepository(db)
	base := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)
	construction := "CONSTRUCTION"
	weather := "WEATHER"
	alert := realtime.AlertSummary{
		EntityID: "sparse-deletion", Cause: &construction,
		InformedEntities: []realtime.InformedEntity{{RouteID: "route-direct"}},
	}
	feed := func(at time.Time, alerts []realtime.AlertSummary, payload string) {
		completeTestRunAt(t, writer, "https://example.test/alerts", payload, realtime.FeedSummary{
			Incrementality: "FULL_DATASET", Timestamp: &realtime.Timestamp{Unix: uint64(at.Unix())},
			EntityCount: len(alerts), AlertCount: len(alerts), Alerts: alerts,
		}, at, false)
	}
	feed(base, []realtime.AlertSummary{alert}, "sparse-content-one")
	alert.Cause = &weather
	alert.InformedEntities = []realtime.InformedEntity{{RouteID: "route-trip"}}
	feed(base.Add(10*time.Minute), []realtime.AlertSummary{alert}, "sparse-content-two")
	feed(base.Add(20*time.Minute), []realtime.AlertSummary{{EntityID: "sparse-deletion", Deleted: true}, {
		EntityID: "deletion-only", Deleted: true,
	}}, "sparse-tombstones")

	from, to := base.Add(-time.Minute), base.Add(5*time.Minute)
	page, err := reader.ListAlerts(ctx, AlertQuery{
		Status: AlertStatusHistorical, Now: base.Add(time.Hour), Page: 1, PageSize: 10,
		LineID: "route-direct", Cause: construction, From: &from, To: &to,
	})
	if err != nil {
		t.Fatalf("ListAlerts(filtered sparse deletion) error = %v", err)
	}
	if len(page.Alerts) != 1 || page.Total != 1 || page.Alerts[0].RevisionNumber != 1 ||
		page.Alerts[0].Cause == nil || *page.Alerts[0].Cause != construction ||
		len(page.Alerts[0].Routes) != 1 || page.Alerts[0].Routes[0].SourceRouteID != "route-direct" {
		t.Errorf("filtered historical page = %#v", page)
	}
	futureFrom, futureTo := base.Add(time.Hour), base.Add(2*time.Hour)
	future, err := reader.ListAlerts(ctx, AlertQuery{
		Status: AlertStatusHistorical, Now: base.Add(time.Hour), Page: 1, PageSize: 10,
		LineID: "route-direct", Cause: construction, From: &futureFrom, To: &futureTo,
	})
	if err != nil {
		t.Fatalf("ListAlerts(future sparse deletion) error = %v", err)
	}
	if len(future.Alerts) != 0 || future.Total != 0 {
		t.Errorf("future historical page = %#v", future)
	}
	unfiltered, err := reader.ListAlerts(ctx, AlertQuery{
		Status: AlertStatusHistorical, Now: base.Add(time.Hour), Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListAlerts(unfiltered sparse deletion) error = %v", err)
	}
	if len(unfiltered.Alerts) != 1 || unfiltered.Alerts[0].RevisionNumber != 2 {
		t.Errorf("unfiltered historical page = %#v", unfiltered)
	}
	var alertID int64
	if err := db.QueryRow("SELECT id FROM service_alerts WHERE source_entity_id = 'sparse-deletion'").Scan(&alertID); err != nil {
		t.Fatalf("query sparse deletion ID: %v", err)
	}
	detail, err := reader.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatalf("GetAlert(sparse deletion) error = %v", err)
	}
	if detail.ClosedAt == nil || !detail.ClosedAt.Equal(base.Add(20*time.Minute)) || !detail.LatestRevision.IsDeleted {
		t.Errorf("sparse deletion detail = %#v", detail)
	}
	var deletionOnlyID int64
	if err := db.QueryRow("SELECT id FROM service_alerts WHERE source_entity_id = 'deletion-only'").Scan(&deletionOnlyID); err != nil {
		t.Fatalf("query deletion-only ID: %v", err)
	}
	if _, err := reader.GetAlert(ctx, deletionOnlyID); err != nil {
		t.Fatalf("GetAlert(deletion-only) error = %v", err)
	}
}

func TestReadRepositoryHistoricalOrderingUsesLifecycleTime(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	writer := NewAlertRepository(db)
	base := time.Date(2026, time.July, 21, 8, 0, 0, 0, time.UTC)
	first := realtime.AlertSummary{EntityID: "deleted-early"}
	second := realtime.AlertSummary{EntityID: "absent-later"}
	feed := func(at time.Time, alerts []realtime.AlertSummary, payload string) {
		completeTestRunAt(t, writer, "https://example.test/alerts", payload, realtime.FeedSummary{
			Incrementality: "FULL_DATASET", Timestamp: &realtime.Timestamp{Unix: uint64(at.Unix())},
			EntityCount: len(alerts), AlertCount: len(alerts), Alerts: alerts,
		}, at, false)
	}
	feed(base, []realtime.AlertSummary{first, second}, "history-initial")
	first.Deleted = true
	feed(base.Add(time.Minute), []realtime.AlertSummary{first, second}, "history-delete")
	feed(base.Add(5*time.Minute), []realtime.AlertSummary{first}, "history-absence")
	feed(base.Add(9*time.Minute), []realtime.AlertSummary{first}, "history-repeat-delete")

	page, err := NewReadRepository(db).ListAlerts(ctx, AlertQuery{
		Status: AlertStatusHistorical, Now: base.Add(10 * time.Minute), Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListAlerts(historical ordering) error = %v", err)
	}
	if len(page.Alerts) != 2 || page.Alerts[0].SourceEntityID != "absent-later" ||
		page.Alerts[1].SourceEntityID != "deleted-early" {
		t.Errorf("historical order = %#v", page.Alerts)
	}
}
