package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

func TestAlertServiceCompletesSuccessfulRun(t *testing.T) {
	store := &fakeAlertStore{runID: 42}
	fetch := realtime.FetchResult{Body: []byte{1, 2, 3}, RetrievedAt: time.Now()}
	summary := realtime.FeedSummary{EntityCount: 2, AlertCount: 2}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: fetch},
		Store:     store,
		Decode:    func([]byte) (realtime.FeedSummary, error) { return summary, nil },
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.RunID != 42 || result.Summary.AlertCount != 2 {
		t.Errorf("result = %#v", result)
	}
	if !store.completed || store.failed {
		t.Errorf("store completed = %t, failed = %t", store.completed, store.failed)
	}
	if store.sourceURL != "https://example.test/alerts" {
		t.Errorf("source URL = %q", store.sourceURL)
	}
}

func TestAlertServiceReportsSkippedRun(t *testing.T) {
	store := &fakeAlertStore{runID: 43, skipped: true}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte{1}}},
		Store:     store,
		Decode: func([]byte) (realtime.FeedSummary, error) {
			return realtime.FeedSummary{Incrementality: "FULL_DATASET"}, nil
		},
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Skipped {
		t.Error("Run() skipped = false, want true")
	}
}

func TestAlertServiceRecordsFetchFailure(t *testing.T) {
	store := &fakeAlertStore{runID: 7}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{err: errors.New("upstream unavailable")},
		Store:     store,
		Decode:    realtime.DecodeAlerts,
	}

	result, err := service.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "upstream unavailable") {
		t.Fatalf("Run() error = %v", err)
	}
	if result.RunID != 7 || !store.failed || store.completed {
		t.Errorf("result = %#v, store completed = %t, failed = %t", result, store.completed, store.failed)
	}
}

func TestAlertServiceRecordsDecodeFailure(t *testing.T) {
	store := &fakeAlertStore{runID: 8}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte("invalid")}},
		Store:     store,
		Decode:    realtime.DecodeAlerts,
	}

	_, err := service.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode GTFS-Realtime feed") {
		t.Fatalf("Run() error = %v", err)
	}
	if !store.failed || store.completed {
		t.Errorf("store completed = %t, failed = %t", store.completed, store.failed)
	}
}

func TestAlertServiceRecordsStorageFailure(t *testing.T) {
	store := &fakeAlertStore{runID: 9, completeErr: errors.New("insert failed")}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte{1}}},
		Store:     store,
		Decode: func([]byte) (realtime.FeedSummary, error) {
			return realtime.FeedSummary{AlertCount: 1}, nil
		},
	}

	_, err := service.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "insert failed") {
		t.Fatalf("Run() error = %v", err)
	}
	if !store.failed {
		t.Error("storage failure was not recorded")
	}
}

func TestAlertServiceDoesNotOverwriteUnknownCommitOutcome(t *testing.T) {
	store := &fakeAlertStore{runID: 10, completeErr: unknownCommitError{}}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte{1}}},
		Store:     store,
		Decode: func([]byte) (realtime.FeedSummary, error) {
			return realtime.FeedSummary{AlertCount: 1}, nil
		},
	}

	_, err := service.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "commit outcome unknown") {
		t.Fatalf("Run() error = %v", err)
	}
	if store.failed {
		t.Error("unknown commit outcome was incorrectly overwritten as failed")
	}
}

type fakeAlertFetcher struct {
	result realtime.FetchResult
	err    error
}

func (f fakeAlertFetcher) FetchAlerts(context.Context) (realtime.FetchResult, error) {
	return f.result, f.err
}

type fakeAlertStore struct {
	runID       int64
	sourceURL   string
	completed   bool
	failed      bool
	skipped     bool
	completeErr error
}

type unknownCommitError struct{}

func (unknownCommitError) Error() string              { return "commit outcome unknown" }
func (unknownCommitError) CommitOutcomeUnknown() bool { return true }

func (s *fakeAlertStore) StartAlertRun(_ context.Context, sourceURL string) (int64, error) {
	s.sourceURL = sourceURL
	return s.runID, nil
}

func (s *fakeAlertStore) FailAlertRun(
	_ context.Context,
	_ int64,
	_ *realtime.FetchResult,
	_ *realtime.FeedSummary,
	_ error,
) error {
	s.failed = true
	return nil
}

func (s *fakeAlertStore) CompleteAlertRun(
	_ context.Context,
	_ int64,
	_ realtime.FetchResult,
	_ realtime.FeedSummary,
) (bool, error) {
	s.completed = s.completeErr == nil
	return s.skipped, s.completeErr
}
