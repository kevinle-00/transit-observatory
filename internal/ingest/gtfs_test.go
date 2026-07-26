package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/gtfs"
	"github.com/kevinle-00/transit-observatory/internal/observability"
	"github.com/kevinle-00/transit-observatory/internal/storage"
)

func TestGTFSServiceCompletesImport(t *testing.T) {
	store := &fakeGTFSStore{importID: 21}
	download := testDownload(t, []byte("outer zip"))
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip",
		Fetcher:   fakeGTFSFetcher{download: download},
		Store:     store,
		Archive:   &fakeArchiveStore{},
		Parse: func(string, string) (gtfs.Dataset, error) {
			return gtfs.Dataset{Summary: gtfs.Summary{RouteCount: 2}}, nil
		},
	}
	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ImportID != 21 || result.Dataset.Summary.RouteCount != 2 || !store.completed {
		t.Errorf("result = %#v, store = %#v", result, store)
	}
	if len(store.events) != 3 || store.events[0] != "archive" || store.events[1] != "skip" || store.events[2] != "complete" {
		t.Errorf("events = %v", store.events)
	}
}

func TestGTFSServiceSkipsBeforeParsing(t *testing.T) {
	store := &fakeGTFSStore{importID: 22, skipBeforeParse: true}
	download := testDownload(t, []byte("duplicate"))
	parsed := false
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip",
		Fetcher:   fakeGTFSFetcher{download: download},
		Store:     store,
		Archive:   &fakeArchiveStore{},
		Parse: func(string, string) (gtfs.Dataset, error) {
			parsed = true
			return gtfs.Dataset{}, nil
		},
	}
	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Skipped || parsed {
		t.Errorf("result skipped = %t, parsed = %t", result.Skipped, parsed)
	}
	if !store.archived {
		t.Error("duplicate was not archived")
	}
}

func TestGTFSServiceRecordsParseFailure(t *testing.T) {
	store := &fakeGTFSStore{importID: 23}
	download := testDownload(t, []byte("invalid"))
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip",
		Fetcher:   fakeGTFSFetcher{download: download},
		Store:     store,
		Archive:   &fakeArchiveStore{},
		Parse: func(string, string) (gtfs.Dataset, error) {
			return gtfs.Dataset{}, errors.New("missing routes.txt")
		},
	}
	_, err := service.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing routes.txt") {
		t.Fatalf("Run() error = %v", err)
	}
	if !store.failed || store.completed {
		t.Errorf("store failed = %t, completed = %t", store.failed, store.completed)
	}
	if !store.archived || store.failure.Stage != "parse" || store.failure.Code != "parse_failed" {
		t.Errorf("archive/failure = %t/%#v", store.archived, store.failure)
	}
}

func TestGTFSServiceArchiveFailuresPreventSkipAndParse(t *testing.T) {
	for _, test := range []struct {
		name      string
		archive   *fakeArchiveStore
		recordErr error
	}{
		{name: "publish", archive: &fakeArchiveStore{err: errors.New("storage offline")}},
		{name: "record", archive: &fakeArchiveStore{}, recordErr: errors.New("link failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeGTFSStore{importID: 24, recordErr: test.recordErr}
			parsed := false
			service := GTFSService{
				SourceURL: "https://example.test/gtfs.zip",
				Fetcher:   fakeGTFSFetcher{download: testDownload(t, []byte("outer zip"))},
				Store:     store,
				Archive:   test.archive,
				Parse: func(string, string) (gtfs.Dataset, error) {
					parsed = true
					return gtfs.Dataset{}, nil
				},
			}
			if _, err := service.Run(context.Background()); err == nil {
				t.Fatal("Run() error = nil")
			}
			if parsed || store.completed || containsEvent(store.events, "skip") || store.failure.Stage != "archive" {
				t.Errorf("parsed/completed/events/failure = %t/%t/%v/%#v", parsed, store.completed, store.events, store.failure)
			}
		})
	}
}

func TestGTFSServiceDoesNotRewriteSuccessForCleanupFailure(t *testing.T) {
	store := &fakeGTFSStore{importID: 25}
	download := testDownload(t, []byte("outer zip"))
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip", Fetcher: fakeGTFSFetcher{download: download},
		Store: store, Archive: &fakeArchiveStore{},
		Parse: func(path, _ string) (gtfs.Dataset, error) {
			if err := os.Chmod(filepath.Dir(path), 0o500); err != nil {
				t.Fatal(err)
			}
			return gtfs.Dataset{}, nil
		},
	}
	defer os.Chmod(filepath.Dir(download.Path), 0o700)
	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v, want cleanup warning only", err)
	}
	if result.CleanupError == nil {
		t.Fatal("Run() CleanupError = nil")
	}
	if !store.completed || store.failed {
		t.Errorf("completed/failed = %t/%t", store.completed, store.failed)
	}
}

func TestGTFSServiceDoesNotFinalizeUnknownArchiveCommit(t *testing.T) {
	store := &fakeGTFSStore{importID: 27, recordErr: unknownCommitError{}}
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip", Fetcher: fakeGTFSFetcher{download: testDownload(t, []byte("outer zip"))},
		Store: store, Archive: &fakeArchiveStore{}, Parse: func(string, string) (gtfs.Dataset, error) { return gtfs.Dataset{}, nil },
	}
	if _, err := service.Run(context.Background()); err == nil || !commitOutcomeUnknown(err) {
		t.Fatalf("Run() error = %v, want unknown commit outcome", err)
	}
	if store.failed {
		t.Error("unknown archive commit was finalized as failed")
	}
}

func TestGTFSServiceDoesNotOverwriteUnknownCommitOutcome(t *testing.T) {
	store := &fakeGTFSStore{importID: 26, completeErr: unknownCommitError{}}
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip", Fetcher: fakeGTFSFetcher{download: testDownload(t, []byte("outer zip"))},
		Store: store, Archive: &fakeArchiveStore{}, Parse: func(string, string) (gtfs.Dataset, error) { return gtfs.Dataset{}, nil },
	}
	if _, err := service.Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil")
	}
	if store.failed {
		t.Error("unknown commit outcome was overwritten")
	}
}

func TestGTFSServiceDoesNotOverwriteUnknownSkipCommitOutcome(t *testing.T) {
	store := &fakeGTFSStore{importID: 28, skipErr: unknownCommitError{}}
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip", Fetcher: fakeGTFSFetcher{download: testDownload(t, []byte("outer zip"))},
		Store: store, Archive: &fakeArchiveStore{}, Parse: func(string, string) (gtfs.Dataset, error) { return gtfs.Dataset{}, nil },
	}
	if _, err := service.Run(context.Background()); err == nil || !commitOutcomeUnknown(err) {
		t.Fatalf("Run() error = %v, want unknown skip commit outcome", err)
	}
	if store.failed {
		t.Error("unknown skip commit outcome was overwritten")
	}
}

type fakeGTFSFetcher struct {
	download gtfs.Download
	err      error
}

func (f fakeGTFSFetcher) Fetch(context.Context) (gtfs.Download, error) {
	return f.download, f.err
}

type fakeGTFSStore struct {
	importID        int64
	skipBeforeParse bool
	completed       bool
	failed          bool
	archived        bool
	failure         observability.Failure
	events          []string
	recordErr       error
	skipErr         error
	completeErr     error
}

func (s *fakeGTFSStore) StartImport(context.Context, string) (int64, error) {
	return s.importID, nil
}

func (s *fakeGTFSStore) SkipIfImported(context.Context, int64, gtfs.Download) (bool, error) {
	s.events = append(s.events, "skip")
	return s.skipBeforeParse, s.skipErr
}

func (s *fakeGTFSStore) RecordGTFSArchive(context.Context, int64, storage.Object) error {
	s.archived = s.recordErr == nil
	s.events = append(s.events, "archive")
	return s.recordErr
}

func (s *fakeGTFSStore) CompleteImport(context.Context, int64, gtfs.Download, gtfs.Dataset) (bool, error) {
	s.completed = true
	s.events = append(s.events, "complete")
	return false, s.completeErr
}

func containsEvent(events []string, target string) bool {
	for _, event := range events {
		if event == target {
			return true
		}
	}
	return false
}

func (s *fakeGTFSStore) FailImportWithFailure(_ context.Context, _ int64, _ *gtfs.Download, failure observability.Failure) error {
	s.failed = true
	s.failure = failure
	return nil
}

func testDownload(t *testing.T, data []byte) gtfs.Download {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "gtfs-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	return gtfs.Download{Path: file.Name(), RetrievedAt: time.Now(), SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data)), ContentType: "application/zip"}
}
