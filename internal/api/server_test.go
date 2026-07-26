package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/database"
)

type stubHealthChecker struct{ err error }

func (checker stubHealthChecker) PingContext(context.Context) error { return checker.err }

type stubReadRepository struct {
	alertPage       database.AlertPage
	alert           database.AlertDetail
	revisions       []database.AlertRevision
	lines           []database.LineSummary
	line            database.LineDetail
	stations        []database.StationSummary
	station         database.StationDetail
	analytics       []database.LineAnalytics
	lineAnalytics   database.LineAnalytics
	err             error
	alertQuery      database.AlertQuery
	alertID         int64
	lineID          string
	stationID       string
	includeBus      bool
	stationQuery    database.StationQuery
	analyticsQuery  database.AnalyticsQuery
	now             time.Time
	blockListAlerts bool
}

func (stub *stubReadRepository) ListAlerts(ctx context.Context, query database.AlertQuery) (database.AlertPage, error) {
	stub.alertQuery = query
	if stub.blockListAlerts {
		<-ctx.Done()
		return database.AlertPage{}, ctx.Err()
	}
	return stub.alertPage, stub.err
}

func (stub *stubReadRepository) GetAlert(_ context.Context, id int64) (database.AlertDetail, error) {
	stub.alertID = id
	return stub.alert, stub.err
}

func (stub *stubReadRepository) ListAlertRevisions(_ context.Context, id int64) ([]database.AlertRevision, error) {
	stub.alertID = id
	return stub.revisions, stub.err
}

func (stub *stubReadRepository) ListLines(_ context.Context, includeBus bool, now time.Time) ([]database.LineSummary, error) {
	stub.includeBus, stub.now = includeBus, now
	return stub.lines, stub.err
}

func (stub *stubReadRepository) GetLine(_ context.Context, id string, now time.Time) (database.LineDetail, error) {
	stub.lineID, stub.now = id, now
	return stub.line, stub.err
}

func (stub *stubReadRepository) ListStations(_ context.Context, query database.StationQuery, now time.Time) ([]database.StationSummary, error) {
	stub.stationQuery, stub.now = query, now
	return stub.stations, stub.err
}

func (stub *stubReadRepository) GetStation(_ context.Context, id string, now time.Time) (database.StationDetail, error) {
	stub.stationID, stub.now = id, now
	return stub.station, stub.err
}

func (stub *stubReadRepository) ListLineAnalytics(_ context.Context, query database.AnalyticsQuery) ([]database.LineAnalytics, error) {
	stub.analyticsQuery = query
	return stub.analytics, stub.err
}

func (stub *stubReadRepository) GetLineAnalytics(_ context.Context, id string, query database.AnalyticsQuery) (database.LineAnalytics, error) {
	stub.lineID, stub.analyticsQuery = id, query
	return stub.lineAnalytics, stub.err
}

func TestHandlerHealth(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "available", wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		{name: "unavailable", err: errors.New("offline"), wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unavailable"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := testHandler(stubHealthChecker{err: test.err}, &stubReadRepository{}, nil)
			response := serve(handler, http.MethodGet, "/health")
			if response.Code != test.wantStatus || strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHandlerListsAlertsWithParsedQueryAndFixedNow(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 26, 12, 30, 0, 0, time.FixedZone("offset", 3600))
	stub := &stubReadRepository{alertPage: database.AlertPage{Alerts: []database.CurrentAlert{{ID: 7}}}}
	handler := testHandler(stubHealthChecker{}, stub, nil)
	handler.now = func() time.Time { return fixedNow }
	response := serve(handler, http.MethodGet, "/api/v1/alerts?status=current&line_id=red%3A1&station_id=s1&cause=OTHER_CAUSE&effect=DETOUR&from=2026-07-01T00%3A00%3A00Z&to=2026-08-01T00%3A00%3A00Z")
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if stub.alertQuery.Status != "current" || stub.alertQuery.LineID != "red:1" || stub.alertQuery.StationID != "s1" || stub.alertQuery.Cause != "OTHER_CAUSE" || stub.alertQuery.Effect != "DETOUR" {
		t.Errorf("query = %+v", stub.alertQuery)
	}
	if !stub.alertQuery.Now.Equal(fixedNow) || stub.alertQuery.Now.Location() != time.UTC {
		t.Errorf("query now = %v", stub.alertQuery.Now)
	}
	assertJSONValue(t, response, "meta", "count", float64(1))
	assertJSONValue(t, response, "meta", "status", "current")
}

func TestHandlerHistoricalAlertMetadata(t *testing.T) {
	stub := &stubReadRepository{alertPage: database.AlertPage{Alerts: []database.CurrentAlert{}, Total: 51}}
	handler := testHandler(stubHealthChecker{}, stub, nil)
	response := serve(handler, http.MethodGet, "/api/v1/alerts?status=historical&page=2&page_size=25")
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if stub.alertQuery.Page != 2 || stub.alertQuery.PageSize != 25 {
		t.Errorf("pagination query = %+v", stub.alertQuery)
	}
	assertJSONValue(t, response, "meta", "total", float64(51))
	assertJSONValue(t, response, "meta", "total_pages", float64(3))
	var envelope struct {
		Data []database.CurrentAlert `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data == nil {
		t.Errorf("empty data was not []: %s", response.Body.String())
	}
}

func TestHandlerAlertDetailsAndRevisions(t *testing.T) {
	stub := &stubReadRepository{
		alert:     database.AlertDetail{ID: 42},
		revisions: []database.AlertRevision{{CurrentAlert: database.CurrentAlert{ID: 42}, IsDeleted: true}},
	}
	handler := testHandler(stubHealthChecker{}, stub, nil)
	response := serve(handler, http.MethodGet, "/api/v1/alerts/42")
	if response.Code != http.StatusOK || stub.alertID != 42 {
		t.Fatalf("detail response = %d %s", response.Code, response.Body.String())
	}
	assertJSONValue(t, response, "data", "id", float64(42))
	response = serve(handler, http.MethodGet, "/api/v1/alerts/42/revisions")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"is_deleted":true`) {
		t.Fatalf("revision response = %d %s", response.Code, response.Body.String())
	}
	assertJSONValue(t, response, "meta", "count", float64(1))
}

func TestHandlerNetworkEndpointsAndEscapedIDs(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubReadRepository{
		lines:    []database.LineSummary{{ID: "line:1"}},
		line:     database.LineDetail{Line: database.LineSummary{ID: "line:1"}, Stations: []database.StationSummary{}, Alerts: []database.CurrentAlert{}},
		stations: []database.StationSummary{{ID: "station:1", Lines: []database.LineSummary{}}},
		station:  database.StationDetail{Station: database.StationSummary{ID: "station:1", Lines: []database.LineSummary{}}, Alerts: []database.CurrentAlert{}},
	}
	handler := testHandler(stubHealthChecker{}, stub, nil)
	handler.now = func() time.Time { return fixedNow }

	if response := serve(handler, http.MethodGet, "/api/v1/lines?include_replacement_bus=true"); response.Code != http.StatusOK || !stub.includeBus {
		t.Fatalf("line list response = %d %s", response.Code, response.Body.String())
	}
	if response := serve(handler, http.MethodGet, "/api/v1/lines/line%3A1"); response.Code != http.StatusOK || stub.lineID != "line:1" {
		t.Fatalf("line detail response = %d %s, id %q", response.Code, response.Body.String(), stub.lineID)
	}
	stub.line = database.LineDetail{Line: database.LineSummary{ID: "line/1"}, Stations: []database.StationSummary{}, Alerts: []database.CurrentAlert{}}
	if response := serve(handler, http.MethodGet, "/api/v1/lines/line%2F1"); response.Code != http.StatusOK || stub.lineID != "line/1" {
		t.Fatalf("escaped slash response = %d %s, id %q", response.Code, response.Body.String(), stub.lineID)
	}
	if response := serve(handler, http.MethodGet, "/api/v1/stations?q=Central&line_id=line%3A1"); response.Code != http.StatusOK {
		t.Fatalf("station list response = %d %s", response.Code, response.Body.String())
	}
	if stub.stationQuery != (database.StationQuery{Q: "Central", LineID: "line:1"}) || stub.now != fixedNow {
		t.Errorf("station query = %+v, now = %v", stub.stationQuery, stub.now)
	}
	if response := serve(handler, http.MethodGet, "/api/v1/stations/station%3A1"); response.Code != http.StatusOK || stub.stationID != "station:1" {
		t.Fatalf("station detail response = %d %s, id %q", response.Code, response.Body.String(), stub.stationID)
	}
}

func TestHandlerAnalyticsDefaultsAndDetail(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.FixedZone("offset", -7*3600))
	stub := &stubReadRepository{analytics: []database.LineAnalytics{}, lineAnalytics: database.LineAnalytics{
		Line: database.LineSummary{ID: "line:1"}, Series: []database.AnalyticsPoint{}, Causes: []database.AnalyticsBreakdown{}, Effects: []database.AnalyticsBreakdown{}, MetricLimitations: []string{"limited"},
	}}
	handler := testHandler(stubHealthChecker{}, stub, nil)
	handler.now = func() time.Time { return fixedNow }
	response := serve(handler, http.MethodGet, "/api/v1/analytics/lines?include_replacement_bus=true")
	if response.Code != http.StatusOK {
		t.Fatalf("list response = %d %s", response.Code, response.Body.String())
	}
	wantTo := fixedNow.UTC()
	if stub.analyticsQuery.Interval != "day" || !stub.analyticsQuery.Now.Equal(wantTo) ||
		!stub.analyticsQuery.To.Equal(wantTo) || !stub.analyticsQuery.From.Equal(wantTo.Add(-30*24*time.Hour)) ||
		!stub.analyticsQuery.IncludeReplacementBus {
		t.Errorf("default query = %+v", stub.analyticsQuery)
	}
	assertJSONValue(t, response, "meta", "timezone", "UTC")
	assertJSONValue(t, response, "meta", "metric_basis", "continuous_feed_observation_episodes")

	response = serve(handler, http.MethodGet, "/api/v1/analytics/lines/line%3A1?from=2026-01-01T00%3A00%3A00Z&to=2026-02-01T00%3A00%3A00Z&interval=week")
	if response.Code != http.StatusOK || stub.lineID != "line:1" || stub.analyticsQuery.Interval != "week" {
		t.Fatalf("detail response = %d %s, query %+v", response.Code, response.Body.String(), stub.analyticsQuery)
	}
	if !stub.analyticsQuery.Now.Equal(wantTo) || stub.analyticsQuery.To.Equal(stub.analyticsQuery.Now) {
		t.Errorf("detail analytics now/to = %s/%s", stub.analyticsQuery.Now, stub.analyticsQuery.To)
	}
	if !strings.Contains(response.Body.String(), `"metric_limitations":["limited"]`) {
		t.Errorf("detail limitations missing: %s", response.Body.String())
	}
}

func TestHandlerRejectsInvalidQueries(t *testing.T) {
	longID := strings.Repeat("x", 257)
	longQ := strings.Repeat("a", 101)
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "chunk 7 future", path: "/api/v1/alerts?future=true"},
		{name: "duplicate", path: "/api/v1/alerts?status=current&status=present"},
		{name: "empty", path: "/api/v1/alerts?cause="},
		{name: "bad status", path: "/api/v1/alerts?status=other"},
		{name: "bad timestamp", path: "/api/v1/alerts?from=yesterday"},
		{name: "reversed alerts", path: "/api/v1/alerts?from=2026-02-01T00%3A00%3A00Z&to=2026-01-01T00%3A00%3A00Z"},
		{name: "pagination on current", path: "/api/v1/alerts?status=current&page=1"},
		{name: "bad page", path: "/api/v1/alerts?status=historical&page=zero"},
		{name: "deep page", path: "/api/v1/alerts?status=historical&page=1001"},
		{name: "large page", path: "/api/v1/alerts?status=historical&page_size=101"},
		{name: "control", path: "/api/v1/alerts?cause=bad%00value"},
		{name: "long filter id", path: "/api/v1/alerts?line_id=" + longID},
		{name: "bad bool", path: "/api/v1/lines?include_replacement_bus=1"},
		{name: "empty q", path: "/api/v1/stations?q="},
		{name: "long q", path: "/api/v1/stations?q=" + longQ},
		{name: "analytics reversed", path: "/api/v1/analytics/lines?from=2026-02-01T00%3A00%3A00Z&to=2026-01-01T00%3A00%3A00Z"},
		{name: "analytics too long", path: "/api/v1/analytics/lines?from=2025-01-01T00%3A00%3A00Z&to=2026-01-03T00%3A00%3A00Z"},
		{name: "bad interval", path: "/api/v1/analytics/lines?interval=month"},
		{name: "detail replacement bus", path: "/api/v1/analytics/lines/red?include_replacement_bus=false"},
		{name: "detail query", path: "/api/v1/lines/red?x=1"},
		{name: "long path id", path: "/api/v1/lines/" + longID},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serve(testHandler(stubHealthChecker{}, &stubReadRepository{}, nil), http.MethodGet, test.path)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_query"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHandlerMapsNotFoundAndInvalidAlertIDs(t *testing.T) {
	for _, path := range []string{"/api/v1/alerts/nope", "/api/v1/alerts/0", "/api/v1/alerts/-1", "/api/v1/alerts/999999999999999999999"} {
		response := serve(testHandler(stubHealthChecker{}, &stubReadRepository{}, nil), http.MethodGet, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s response = %d %s", path, response.Code, response.Body.String())
		}
	}
	stub := &stubReadRepository{err: database.ErrNotFound}
	for _, path := range []string{"/api/v1/alerts/1", "/api/v1/alerts/1/revisions", "/api/v1/lines/red", "/api/v1/stations/s1", "/api/v1/analytics/lines/red"} {
		response := serve(testHandler(stubHealthChecker{}, stub, nil), http.MethodGet, path)
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
			t.Errorf("%s response = %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestHandlerTimeoutAndInternalErrorsAreSanitized(t *testing.T) {
	stub := &stubReadRepository{blockListAlerts: true}
	handler := testHandler(stubHealthChecker{}, stub, nil)
	handler.requestTimeout = time.Millisecond
	response := serve(handler, http.MethodGet, "/api/v1/alerts")
	if response.Code != http.StatusGatewayTimeout || !strings.Contains(response.Body.String(), `"code":"request_timeout"`) {
		t.Fatalf("timeout response = %d %s", response.Code, response.Body.String())
	}

	var logs bytes.Buffer
	stub = &stubReadRepository{err: errors.New("private database detail")}
	response = serve(testHandler(stubHealthChecker{}, stub, &logs), http.MethodGet, "/api/v1/lines")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private") || !strings.Contains(logs.String(), "private database detail") {
		t.Fatalf("internal response = %d %s, logs %s", response.Code, response.Body.String(), logs.String())
	}
}

func TestHandlerBuffersMarshalBeforeStatus(t *testing.T) {
	notANumber := math.NaN()
	stub := &stubReadRepository{stations: []database.StationSummary{{Latitude: &notANumber, Lines: []database.LineSummary{}}}}
	response := serve(testHandler(stubHealthChecker{}, stub, nil), http.MethodGet, "/api/v1/stations")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), `"data"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestHandlerRoutingMethodPrecedenceAndExactPaths(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodPost, path: "/api/v1/missing", status: http.StatusNotFound},
		{method: http.MethodOptions, path: "/api/v1/missing", status: http.StatusNotFound},
		{method: http.MethodPost, path: "/api/v1/alerts/1", status: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/api/v1/alerts/", status: http.StatusNotFound},
		{method: http.MethodGet, path: "/api/v1/alerts/1/", status: http.StatusNotFound},
	} {
		response := serve(testHandler(stubHealthChecker{}, &stubReadRepository{}, nil), test.method, test.path)
		if response.Code != test.status {
			t.Errorf("%s %s response = %d %s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}

func TestHandlerRejectsServeMuxCanonicalRedirectPaths(t *testing.T) {
	handler := testHandler(stubHealthChecker{}, &stubReadRepository{}, nil)
	for _, path := range []string{
		"/api//v1/alerts",
		"/api/v1/./alerts",
		"/api/v1/lines/../alerts",
		"/api/v1/lines/%2e%2e/alerts",
	} {
		response := serve(handler, http.MethodGet, path)
		if response.Code != http.StatusNotFound || response.Header().Get("Location") != "" ||
			!strings.Contains(response.Body.String(), `"code":"not_found"`) {
			t.Errorf("%s response = %d, location %q, body %s", path, response.Code,
				response.Header().Get("Location"), response.Body.String())
		}
	}
}

func TestHandlerOptionsAndCORSOnDynamicRoute(t *testing.T) {
	handler := testHandler(stubHealthChecker{}, &stubReadRepository{}, nil)
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/lines/red%3A1", nil)
	request.Header.Set("Origin", "https://web.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://web.example" || response.Header().Get("Access-Control-Allow-Methods") != "GET, OPTIONS" {
		t.Fatalf("preflight response = %d, headers %v", response.Code, response.Header())
	}
	plain := serve(handler, http.MethodOptions, "/api/v1/stations/station%3A1")
	if plain.Code != http.StatusNoContent || plain.Header().Get("Allow") != "GET, OPTIONS" {
		t.Fatalf("plain OPTIONS response = %d, headers %v", plain.Code, plain.Header())
	}
}

func testHandler(health HealthChecker, reads ReadRepository, logs *bytes.Buffer) *Handler {
	if logs == nil {
		logs = &bytes.Buffer{}
	}
	return NewHandler(health, reads, slog.New(slog.NewJSONHandler(logs, nil)), "https://web.example", time.Second)
}

func serve(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

func assertJSONValue(t *testing.T, response *httptest.ResponseRecorder, object, key string, want any) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := body[object].(map[string]any)[key]
	if got != want {
		t.Errorf("%s.%s = %#v, want %#v", object, key, got, want)
	}
}
