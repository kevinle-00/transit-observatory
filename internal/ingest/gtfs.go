package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/gtfs"
	"github.com/kevinle-00/transit-observatory/internal/observability"
	"github.com/kevinle-00/transit-observatory/internal/storage"
)

type GTFSFetcher interface {
	Fetch(context.Context) (gtfs.Download, error)
}

type GTFSStore interface {
	StartImport(context.Context, string) (int64, error)
	RecordGTFSArchive(context.Context, int64, storage.Object) error
	SkipIfImported(context.Context, int64, gtfs.Download) (bool, error)
	CompleteImport(context.Context, int64, gtfs.Download, gtfs.Dataset) (bool, error)
	FailImportWithFailure(context.Context, int64, *gtfs.Download, observability.Failure) error
}

type GTFSResult struct {
	ImportID      int64
	Download      gtfs.Download
	Dataset       gtfs.Dataset
	Archive       storage.Object
	ParseDuration time.Duration
	Skipped       bool
	CleanupError  error `json:"-"`
}

type GTFSService struct {
	SourceURL string
	TempDir   string
	Fetcher   GTFSFetcher
	Store     GTFSStore
	Archive   storage.Store
	Parse     func(string, string) (gtfs.Dataset, error)
}

func (s GTFSService) Run(ctx context.Context) (result GTFSResult, runErr error) {
	importID, err := s.Store.StartImport(ctx, s.SourceURL)
	if err != nil {
		return GTFSResult{}, err
	}
	download, err := s.Fetcher.Fetch(ctx)
	if err != nil {
		return GTFSResult{ImportID: importID}, s.fail(ctx, importID, nil, failure("fetch", "fetch_failed", "GTFS fetch failed", err))
	}
	defer func() {
		if err := download.Cleanup(); err != nil {
			if runErr != nil {
				runErr = errors.Join(runErr, err)
			} else {
				result.CleanupError = err
			}
		}
	}()
	result = GTFSResult{ImportID: importID, Download: download}
	source, err := storage.FileSource(download.Path)
	if err != nil {
		return result, s.fail(ctx, importID, &download, failure("archive", "archive_failed", "GTFS archive failed", err))
	}
	key, err := storage.GTFSKey(download.RetrievedAt, download.SHA256)
	if err != nil {
		return result, s.fail(ctx, importID, &download, failure("archive", "archive_failed", "GTFS archive failed", err))
	}
	archive, err := s.Archive.Put(ctx, storage.PutRequest{
		Key: key, Source: source, Size: download.Size, SHA256: download.SHA256, ContentType: download.ContentType,
	})
	if err != nil {
		return result, s.fail(ctx, importID, &download, failure("archive", "archive_failed", "GTFS archive failed", err))
	}
	result.Archive = archive
	if err := s.Store.RecordGTFSArchive(ctx, importID, archive); err != nil {
		if commitOutcomeUnknown(err) {
			return result, err
		}
		return result, s.fail(ctx, importID, &download, failure("archive", "archive_failed", "GTFS archive failed", err))
	}

	skipped, err := s.Store.SkipIfImported(ctx, importID, download)
	if err != nil {
		if commitOutcomeUnknown(err) {
			return result, err
		}
		return result, s.fail(ctx, importID, &download, failure("persist", "persist_failed", "GTFS persistence failed", err))
	}
	if skipped {
		result.Skipped = true
		return result, nil
	}

	parseStarted := time.Now()
	dataset, err := s.Parse(download.Path, s.TempDir)
	parseDuration := time.Since(parseStarted)
	result.ParseDuration = parseDuration
	if err != nil {
		return result, s.fail(ctx, importID, &download, failure("parse", "parse_failed", "GTFS parse failed", err))
	}
	result.Dataset = dataset
	skipped, err = s.Store.CompleteImport(ctx, importID, download, dataset)
	if err != nil {
		if commitOutcomeUnknown(err) {
			return result, err
		}
		return result, s.fail(ctx, importID, &download, failure("persist", "persist_failed", "GTFS persistence failed", err))
	}
	result.Skipped = skipped
	return result, nil
}

func (s GTFSService) fail(ctx context.Context, importID int64, download *gtfs.Download, failure observability.Failure) error {
	failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.Store.FailImportWithFailure(failureContext, importID, download, failure); err != nil {
		return errors.Join(failure, fmt.Errorf("record GTFS import failure: %w", err))
	}
	return failure
}
