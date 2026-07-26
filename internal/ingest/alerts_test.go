package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/observability"
	"github.com/kevinle-00/transit-observatory/internal/realtime"
	"github.com/kevinle-00/transit-observatory/internal/storage"
)

func TestAlertServiceCompletesSuccessfulRun(t *testing.T) {
	store := &fakeAlertStore{runID: 42}
	archive := &fakeArchiveStore{}
	fetch := realtime.FetchResult{Body: []byte{1, 2, 3}, RetrievedAt: time.Now()}
	summary := realtime.FeedSummary{EntityCount: 2, AlertCount: 2}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: fetch},
		Store:     store,
		Archive:   archive,
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
	if string(archive.bytes) != string(fetch.Body) || store.events[0] != "archive" || store.events[1] != "complete" {
		t.Errorf("archive bytes/events = %v/%v", archive.bytes, store.events)
	}
	if archive.request.Size != int64(len(fetch.Body)) || archive.request.ContentType != fetch.ContentType || archive.request.SHA256 == "" {
		t.Errorf("archive request = %#v", archive.request)
	}
}

func TestAlertServiceReportsSkippedRun(t *testing.T) {
	store := &fakeAlertStore{runID: 43, skipped: true}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte{1}, RetrievedAt: time.Now()}},
		Store:     store,
		Archive:   &fakeArchiveStore{},
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
		Archive:   &fakeArchiveStore{},
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
	archive := &fakeArchiveStore{}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte("invalid"), RetrievedAt: time.Now()}},
		Store:     store,
		Archive:   archive,
		Decode:    realtime.DecodeAlerts,
	}

	_, err := service.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode GTFS-Realtime feed") {
		t.Fatalf("Run() error = %v", err)
	}
	if !store.failed || store.completed {
		t.Errorf("store completed = %t, failed = %t", store.completed, store.failed)
	}
	if !store.archived || store.failure.Stage != "decode" || store.failure.Code != "decode_failed" {
		t.Errorf("archive/failure = %t/%#v", store.archived, store.failure)
	}
}

func TestAlertServiceRecordsStorageFailure(t *testing.T) {
	store := &fakeAlertStore{runID: 9, completeErr: errors.New("insert failed")}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte{1}, RetrievedAt: time.Now()}},
		Store:     store,
		Archive:   &fakeArchiveStore{},
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
	if store.failure.Stage != "persist" || store.failure.Code != "persist_failed" {
		t.Errorf("failure = %#v", store.failure)
	}
}

func TestAlertServiceArchiveFailuresPreventDecodeAndPersistence(t *testing.T) {
	for _, test := range []struct {
		name      string
		archive   *fakeArchiveStore
		recordErr error
	}{
		{name: "publish", archive: &fakeArchiveStore{err: errors.New("storage offline")}},
		{name: "record", archive: &fakeArchiveStore{}, recordErr: errors.New("link failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeAlertStore{runID: 11, recordErr: test.recordErr}
			decoded := false
			service := AlertService{
				SourceURL: "https://example.test/alerts",
				Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte("raw"), RetrievedAt: time.Now()}},
				Store:     store,
				Archive:   test.archive,
				Decode: func([]byte) (realtime.FeedSummary, error) {
					decoded = true
					return realtime.FeedSummary{}, nil
				},
			}
			if _, err := service.Run(context.Background()); err == nil {
				t.Fatal("Run() error = nil")
			}
			if decoded || store.completed || store.failure.Stage != "archive" || store.failure.Code != "archive_failed" {
				t.Errorf("decoded/completed/failure = %t/%t/%#v", decoded, store.completed, store.failure)
			}
		})
	}
}

func TestAlertServiceDoesNotOverwriteUnknownCommitOutcome(t *testing.T) {
	store := &fakeAlertStore{runID: 10, completeErr: unknownCommitError{}}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte{1}, RetrievedAt: time.Now()}},
		Store:     store,
		Archive:   &fakeArchiveStore{},
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

func TestAlertServiceDoesNotFinalizeUnknownArchiveCommit(t *testing.T) {
	store := &fakeAlertStore{runID: 12, recordErr: unknownCommitError{}}
	service := AlertService{
		SourceURL: "https://example.test/alerts",
		Fetcher:   fakeAlertFetcher{result: realtime.FetchResult{Body: []byte{1}, RetrievedAt: time.Now()}},
		Store:     store,
		Archive:   &fakeArchiveStore{},
		Decode:    func([]byte) (realtime.FeedSummary, error) { return realtime.FeedSummary{}, nil },
	}
	if _, err := service.Run(context.Background()); err == nil || !commitOutcomeUnknown(err) {
		t.Fatalf("Run() error = %v, want unknown commit outcome", err)
	}
	if store.failed {
		t.Error("unknown archive commit was finalized as failed")
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
	archived    bool
	recordErr   error
	failure     observability.Failure
	events      []string
}

type unknownCommitError struct{}

func (unknownCommitError) Error() string              { return "commit outcome unknown" }
func (unknownCommitError) CommitOutcomeUnknown() bool { return true }

func (s *fakeAlertStore) StartAlertRun(_ context.Context, sourceURL string) (int64, error) {
	s.sourceURL = sourceURL
	return s.runID, nil
}

func (s *fakeAlertStore) FailAlertRunWithFailure(
	_ context.Context,
	_ int64,
	_ *realtime.FetchResult,
	_ *realtime.FeedSummary,
	failure observability.Failure,
) error {
	s.failed = true
	s.failure = failure
	return nil
}

func (s *fakeAlertStore) RecordAlertArchive(_ context.Context, _ int64, _ storage.Object) error {
	s.events = append(s.events, "archive")
	if s.recordErr == nil {
		s.archived = true
	}
	return s.recordErr
}

func (s *fakeAlertStore) CompleteAlertRun(
	_ context.Context,
	_ int64,
	_ realtime.FetchResult,
	_ realtime.FeedSummary,
) (bool, error) {
	s.events = append(s.events, "complete")
	s.completed = s.completeErr == nil
	return s.skipped, s.completeErr
}

type fakeArchiveStore struct {
	bytes   []byte
	err     error
	request storage.PutRequest
}

func (s *fakeArchiveStore) Put(_ context.Context, request storage.PutRequest) (storage.Object, error) {
	s.request = request
	if s.err != nil {
		return storage.Object{}, s.err
	}
	source, err := request.Source.Open()
	if err != nil {
		return storage.Object{}, err
	}
	defer source.Close()
	s.bytes, err = io.ReadAll(source)
	if err != nil {
		return storage.Object{}, err
	}
	hash := sha256.Sum256(s.bytes)
	return storage.Object{Backend: "test", Key: request.Key, Size: int64(len(s.bytes)), SHA256: hex.EncodeToString(hash[:]), StoredAt: time.Now(), Created: storage.Created(true)}, nil
}
