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
	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

type stubHealthChecker struct {
	err error
}

func (checker stubHealthChecker) PingContext(context.Context) error {
	return checker.err
}

type stubAlertLister struct {
	alerts []database.CurrentAlert
	err    error
}

type blockingAlertLister struct{}

func (blockingAlertLister) List(ctx context.Context) ([]database.CurrentAlert, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (lister stubAlertLister) List(context.Context) ([]database.CurrentAlert, error) {
	return lister.alerts, lister.err
}

func TestHandlerHealth(t *testing.T) {
	tests := []struct {
		name       string
		checkError error
		wantStatus int
		wantBody   string
	}{
		{name: "available", wantStatus: http.StatusOK, wantBody: `{"status":"ok"}`},
		{name: "unavailable", checkError: errors.New("database offline"), wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"unavailable"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := testHandler(stubHealthChecker{err: test.checkError}, stubAlertLister{}, "http://localhost:5173", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Errorf("body = %q, want %q", response.Body.String(), test.wantBody)
			}
		})
	}
}

func TestHandlerListsCurrentAlerts(t *testing.T) {
	observedAt := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	header := "Signal fault"
	staticRouteID := "route-1"
	handler := testHandler(stubHealthChecker{}, stubAlertLister{alerts: []database.CurrentAlert{
		{
			ID:                  7,
			SourceURL:           "https://example.test/alerts",
			SourceEntityID:      "alert-1",
			RevisionID:          9,
			RevisionNumber:      2,
			Header:              []realtime.Translation{{Text: header, Language: "en"}},
			Description:         []realtime.Translation{},
			URL:                 []realtime.Translation{},
			FirstSeenAt:         observedAt,
			LastSeenAt:          observedAt,
			RevisionFirstSeenAt: observedAt,
			RevisionLastSeenAt:  observedAt,
			ActivePeriods:       []database.CurrentAlertActivePeriod{},
			Routes: []database.CurrentAlertRoute{
				{SourceRouteID: "route-1", StaticRouteID: &staticRouteID, IsMatched: true},
			},
			Stations: []database.CurrentAlertStation{},
		},
	}}, "http://localhost:5173", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
	}
	var result alertsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Meta.Count != 1 || len(result.Data) != 1 {
		t.Fatalf("response counts = meta %d, data %d", result.Meta.Count, len(result.Data))
	}
	if result.Data[0].SourceEntityID != "alert-1" || len(result.Data[0].Routes) != 1 {
		t.Errorf("alert response = %+v", result.Data[0])
	}
}

func TestHandlerReturnsStableInternalError(t *testing.T) {
	var logs bytes.Buffer
	handler := testHandler(stubHealthChecker{}, stubAlertLister{err: errors.New("private database detail")}, "*", &logs)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	request.Header.Set("Origin", "https://web.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", response.Code)
	}
	if strings.Contains(response.Body.String(), "private database detail") {
		t.Errorf("response exposed internal error: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"internal_error"`) {
		t.Errorf("response = %s", response.Body.String())
	}
	if !strings.Contains(logs.String(), "private database detail") {
		t.Errorf("internal error was not logged: %s", logs.String())
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", response.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestHandlerTimesOutAlertQuery(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	handler := NewHandler(stubHealthChecker{}, blockingAlertLister{}, logger, "*", time.Millisecond)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil))
	if response.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"request_timeout"`) {
		t.Errorf("response = %s", response.Body.String())
	}
}

func TestHandlerDoesNotCommitSuccessBeforeEncoding(t *testing.T) {
	notANumber := math.NaN()
	handler := testHandler(stubHealthChecker{}, stubAlertLister{alerts: []database.CurrentAlert{
		{Stations: []database.CurrentAlertStation{{Latitude: &notANumber}}},
	}}, "*", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil))
	if response.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"data"`) || !strings.Contains(response.Body.String(), `"code":"internal_error"`) {
		t.Errorf("response = %s", response.Body.String())
	}
}

func TestHandlerRoutingErrors(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "not found", method: http.MethodGet, path: "/api/v1/missing", wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "unknown method and route", method: http.MethodPost, path: "/api/v1/missing", wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "method", method: http.MethodPost, path: "/api/v1/alerts", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "unsupported query", method: http.MethodGet, path: "/api/v1/alerts?future=true", wantStatus: http.StatusBadRequest, wantCode: "invalid_query"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := testHandler(stubHealthChecker{}, stubAlertLister{}, "http://localhost:5173", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Errorf("response = %d %s", response.Code, response.Body.String())
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q", response.Header().Get("X-Content-Type-Options"))
			}
		})
	}
}

func TestHandlerCORSPreflight(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		path       string
		headers    string
		wantStatus int
	}{
		{name: "allowed", origin: "https://web.example", path: "/api/v1/alerts", wantStatus: http.StatusNoContent},
		{name: "content type allowed", origin: "https://web.example", path: "/api/v1/alerts", headers: "content-type", wantStatus: http.StatusNoContent},
		{name: "authorization denied", origin: "https://web.example", path: "/api/v1/alerts", headers: "authorization", wantStatus: http.StatusForbidden},
		{name: "denied", origin: "https://other.example", path: "/api/v1/alerts", wantStatus: http.StatusForbidden},
		{name: "unknown route", origin: "https://web.example", path: "/api/v1/missing", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := testHandler(stubHealthChecker{}, stubAlertLister{}, "https://web.example", nil)
			request := httptest.NewRequest(http.MethodOptions, test.path, nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Access-Control-Request-Method", http.MethodGet)
			if test.headers != "" {
				request.Header.Set("Access-Control-Request-Headers", test.headers)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusNoContent && response.Header().Get("Access-Control-Allow-Origin") != test.origin {
				t.Errorf("Access-Control-Allow-Origin = %q", response.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestHandlerPlainOptions(t *testing.T) {
	handler := testHandler(stubHealthChecker{}, stubAlertLister{}, "https://web.example", nil)
	for _, origin := range []string{"", "https://web.example"} {
		request := httptest.NewRequest(http.MethodOptions, "/api/v1/alerts", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Errorf("origin %q status = %d, want 204: %s", origin, response.Code, response.Body.String())
		}
		if response.Header().Get("Allow") != "GET, OPTIONS" {
			t.Errorf("origin %q Allow = %q", origin, response.Header().Get("Allow"))
		}
	}
}

func testHandler(health HealthChecker, alerts AlertLister, origin string, logs *bytes.Buffer) http.Handler {
	if logs == nil {
		logs = &bytes.Buffer{}
	}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return NewHandler(health, alerts, logger, origin, time.Second)
}
