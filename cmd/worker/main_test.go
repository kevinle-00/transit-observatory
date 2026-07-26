package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"github.com/kevinle-00/transit-observatory/internal/ingest"
	"google.golang.org/protobuf/proto"
)

func TestRunWritesReportToStdoutAndLogsToStderr(t *testing.T) {
	version := "2.0"
	payload, err := proto.Marshal(&gtfs.FeedMessage{
		Header: &gtfs.FeedHeader{GtfsRealtimeVersion: &version},
	})
	if err != nil {
		t.Fatalf("marshal test feed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("KeyID") != "secret-value" {
			t.Errorf("KeyID header = %q, want configured API key", request.Header.Get("KeyID"))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	values := map[string]string{
		"TRANSIT_API_KEY":     "secret-value",
		"TRANSIT_ALERTS_URL":  server.URL,
		"RAW_STORAGE_BACKEND": "invalid-but-unused-by-dry-run",
	}
	getenv := func(key string) string { return values[key] }
	var output bytes.Buffer
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	if err := run(context.Background(), []string{"ingest-alerts", "--dry-run"}, getenv, &output, logger); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !json.Valid(output.Bytes()) {
		t.Fatalf("stdout is not valid JSON: %s", output.String())
	}
	if !strings.Contains(logs.String(), "fetching service alerts") ||
		!strings.Contains(logs.String(), "service-alert dry run completed") {
		t.Errorf("logs do not contain worker lifecycle events: %s", logs.String())
	}
	if strings.Contains(output.String(), "fetching service alerts") {
		t.Errorf("stdout contains logs: %s", output.String())
	}
	if strings.Contains(logs.String(), "secret-value") || strings.Contains(output.String(), "secret-value") {
		t.Error("worker output exposed the API key")
	}
}

func TestLogGTFSCleanupWarningDoesNotExposePath(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	logGTFSCleanupWarning(logger, ingest.GTFSResult{ImportID: 42, CleanupError: errors.New("remove /private/tmp/feed.zip: denied")})
	if !strings.Contains(logs.String(), "static GTFS temporary cleanup failed") || !strings.Contains(logs.String(), `"gtfs_import_id":42`) {
		t.Fatalf("cleanup warning = %s", logs.String())
	}
	if strings.Contains(logs.String(), "/private/tmp/feed.zip") {
		t.Fatalf("cleanup warning exposed path: %s", logs.String())
	}
}
