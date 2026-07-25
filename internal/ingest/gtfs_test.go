package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kevinle-00/transit-observatory/internal/gtfs"
)

func TestGTFSServiceCompletesImport(t *testing.T) {
	store := &fakeGTFSStore{importID: 21}
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip",
		Fetcher:   fakeGTFSFetcher{download: gtfs.Download{SHA256: "hash"}},
		Store:     store,
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
}

func TestGTFSServiceSkipsBeforeParsing(t *testing.T) {
	store := &fakeGTFSStore{importID: 22, skipBeforeParse: true}
	parsed := false
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip",
		Fetcher:   fakeGTFSFetcher{download: gtfs.Download{SHA256: "duplicate"}},
		Store:     store,
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
}

func TestGTFSServiceRecordsParseFailure(t *testing.T) {
	store := &fakeGTFSStore{importID: 23}
	service := GTFSService{
		SourceURL: "https://example.test/gtfs.zip",
		Fetcher:   fakeGTFSFetcher{download: gtfs.Download{SHA256: "invalid"}},
		Store:     store,
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
}

func (s *fakeGTFSStore) StartImport(context.Context, string) (int64, error) {
	return s.importID, nil
}

func (s *fakeGTFSStore) SkipIfImported(context.Context, int64, gtfs.Download) (bool, error) {
	return s.skipBeforeParse, nil
}

func (s *fakeGTFSStore) CompleteImport(context.Context, int64, gtfs.Download, gtfs.Dataset) (bool, error) {
	s.completed = true
	return false, nil
}

func (s *fakeGTFSStore) FailImport(context.Context, int64, *gtfs.Download, error) error {
	s.failed = true
	return nil
}
