package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/gtfs"
)

type GTFSFetcher interface {
	Fetch(context.Context) (gtfs.Download, error)
}

type GTFSStore interface {
	StartImport(context.Context, string) (int64, error)
	SkipIfImported(context.Context, int64, gtfs.Download) (bool, error)
	CompleteImport(context.Context, int64, gtfs.Download, gtfs.Dataset) (bool, error)
	FailImport(context.Context, int64, *gtfs.Download, error) error
}

type GTFSResult struct {
	ImportID      int64
	Download      gtfs.Download
	Dataset       gtfs.Dataset
	ParseDuration time.Duration
	Skipped       bool
}

type GTFSService struct {
	SourceURL string
	TempDir   string
	Fetcher   GTFSFetcher
	Store     GTFSStore
	Parse     func(string, string) (gtfs.Dataset, error)
}

func (s GTFSService) Run(ctx context.Context) (result GTFSResult, runErr error) {
	importID, err := s.Store.StartImport(ctx, s.SourceURL)
	if err != nil {
		return GTFSResult{}, err
	}
	download, err := s.Fetcher.Fetch(ctx)
	if err != nil {
		return GTFSResult{ImportID: importID}, s.fail(ctx, importID, nil, err)
	}
	defer func() {
		if err := download.Cleanup(); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()

	skipped, err := s.Store.SkipIfImported(ctx, importID, download)
	if err != nil {
		return GTFSResult{ImportID: importID, Download: download}, s.fail(ctx, importID, &download, err)
	}
	if skipped {
		return GTFSResult{ImportID: importID, Download: download, Skipped: true}, nil
	}

	parseStarted := time.Now()
	dataset, err := s.Parse(download.Path, s.TempDir)
	parseDuration := time.Since(parseStarted)
	if err != nil {
		return GTFSResult{ImportID: importID, Download: download, ParseDuration: parseDuration},
			s.fail(ctx, importID, &download, err)
	}
	skipped, err = s.Store.CompleteImport(ctx, importID, download, dataset)
	if err != nil {
		if commitOutcomeUnknown(err) {
			return GTFSResult{ImportID: importID, Download: download, Dataset: dataset, ParseDuration: parseDuration}, err
		}
		return GTFSResult{ImportID: importID, Download: download, Dataset: dataset, ParseDuration: parseDuration},
			s.fail(ctx, importID, &download, err)
	}
	return GTFSResult{
		ImportID:      importID,
		Download:      download,
		Dataset:       dataset,
		ParseDuration: parseDuration,
		Skipped:       skipped,
	}, nil
}

func (s GTFSService) fail(ctx context.Context, importID int64, download *gtfs.Download, importError error) error {
	failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.Store.FailImport(failureContext, importID, download, importError); err != nil {
		return errors.Join(importError, fmt.Errorf("record GTFS import failure: %w", err))
	}
	return importError
}
