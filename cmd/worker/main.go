package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/config"
	"github.com/kevinle-00/transit-observatory/internal/database"
	staticgtfs "github.com/kevinle-00/transit-observatory/internal/gtfs"
	"github.com/kevinle-00/transit-observatory/internal/ingest"
	"github.com/kevinle-00/transit-observatory/internal/realtime"
	"github.com/kevinle-00/transit-observatory/internal/storage"
)

type dryRunReport struct {
	SourceURL   string               `json:"source_url"`
	RetrievedAt time.Time            `json:"retrieved_at"`
	HTTPStatus  int                  `json:"http_status"`
	ContentType string               `json:"content_type,omitempty"`
	PayloadSize int                  `json:"payload_size_bytes"`
	Feed        realtime.FeedSummary `json:"feed"`
}

type ingestionReport struct {
	RunID            int64               `json:"ingestion_run_id"`
	Status           string              `json:"status"`
	RetrievedAt      time.Time           `json:"retrieved_at"`
	PayloadSize      int                 `json:"payload_size_bytes"`
	FeedTimestamp    *realtime.Timestamp `json:"feed_timestamp,omitempty"`
	EntityCount      int                 `json:"entity_count"`
	AlertCount       int                 `json:"alert_count"`
	ArchiveObjectKey string              `json:"archive_object_key"`
}

type gtfsImportReport struct {
	ImportID         int64                        `json:"gtfs_import_id,omitempty"`
	Status           string                       `json:"status"`
	SourceURL        string                       `json:"source_url"`
	RetrievedAt      time.Time                    `json:"retrieved_at"`
	ContentSHA256    string                       `json:"content_sha256"`
	ArchiveBytes     int64                        `json:"archive_bytes"`
	ParseDurationMS  int64                        `json:"parse_duration_ms"`
	Summary          staticgtfs.Summary           `json:"summary"`
	Coverage         *database.IdentifierCoverage `json:"realtime_identifier_coverage,omitempty"`
	ArchiveObjectKey string                       `json:"archive_object_key,omitempty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, logger); err != nil {
		logger.Error("worker failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, output io.Writer, logger *slog.Logger) error {
	if len(args) == 0 {
		return errors.New("usage: worker <ingest-alerts|ingest-gtfs|migrate> [options]")
	}
	switch args[0] {
	case "ingest-alerts":
		return runIngestAlerts(ctx, args[1:], getenv, output, logger)
	case "ingest-gtfs":
		return runIngestGTFS(ctx, args[1:], getenv, output, logger)
	case "migrate":
		return runMigrate(ctx, args[1:], getenv, logger)
	default:
		return fmt.Errorf("unknown command %q; usage: worker <ingest-alerts|ingest-gtfs|migrate> [options]", args[0])
	}
}

func runIngestGTFS(ctx context.Context, args []string, getenv func(string) string, output io.Writer, logger *slog.Logger) error {
	flags := flag.NewFlagSet("ingest-gtfs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "download and validate Metro GTFS without persisting it")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse ingest-gtfs flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	gtfsConfig, err := config.LoadGTFS(getenv)
	if err != nil {
		return err
	}
	downloader := staticgtfs.Downloader{
		HTTPClient: &http.Client{Timeout: gtfsConfig.HTTPTimeout},
		URL:        gtfsConfig.URL,
	}
	logger.Info("fetching static GTFS", "url", gtfsConfig.URL, "dry_run", *dryRun)
	if *dryRun {
		download, err := downloader.Fetch(ctx)
		if err != nil {
			return err
		}
		parseStarted := time.Now()
		dataset, parseErr := staticgtfs.ParseArchive(download.Path, "")
		cleanupErr := download.Cleanup()
		if err := errors.Join(parseErr, cleanupErr); err != nil {
			return err
		}
		report := gtfsImportReport{
			Status:          "dry-run",
			SourceURL:       gtfsConfig.URL,
			RetrievedAt:     download.RetrievedAt,
			ContentSHA256:   download.SHA256,
			ArchiveBytes:    download.Size,
			ParseDurationMS: time.Since(parseStarted).Milliseconds(),
			Summary:         dataset.Summary,
		}
		if err := writeJSON(output, report); err != nil {
			return err
		}
		logger.Info("static GTFS dry run completed", "routes", dataset.Summary.RouteCount,
			"stops", dataset.Summary.StopCount, "route_stations", dataset.Summary.RouteStationCount)
		return nil
	}

	databaseConfig, err := config.LoadDatabase(getenv)
	if err != nil {
		return err
	}
	rawStorageConfig, err := config.LoadRawStorage(getenv)
	if err != nil {
		return err
	}
	archiveStore, err := storage.New(ctx, rawStorageConfig)
	if err != nil {
		return fmt.Errorf("configure raw storage: %w", err)
	}
	db, err := database.Open(ctx, databaseConfig.URL)
	if err != nil {
		return err
	}
	defer db.Close()
	repository := database.NewGTFSRepository(db)
	service := ingest.GTFSService{
		SourceURL: gtfsConfig.URL,
		Fetcher:   downloader,
		Store:     repository,
		Archive:   archiveStore,
		Parse:     staticgtfs.ParseArchive,
	}
	result, err := service.Run(ctx)
	if err != nil {
		return fmt.Errorf("ingest static GTFS: %w", err)
	}
	logGTFSCleanupWarning(logger, result)
	coverage, err := repository.Coverage(ctx)
	if err != nil {
		return err
	}
	status := "succeeded"
	if result.Skipped {
		status = "skipped"
		currentSummary, err := repository.CurrentSummary(ctx)
		if err != nil {
			return err
		}
		result.Dataset.Summary = currentSummary
	}
	report := gtfsImportReport{
		ImportID:         result.ImportID,
		Status:           status,
		SourceURL:        gtfsConfig.URL,
		RetrievedAt:      result.Download.RetrievedAt,
		ContentSHA256:    result.Download.SHA256,
		ArchiveBytes:     result.Download.Size,
		ParseDurationMS:  result.ParseDuration.Milliseconds(),
		Summary:          result.Dataset.Summary,
		Coverage:         &coverage,
		ArchiveObjectKey: result.Archive.Key,
	}
	if err := writeJSON(output, report); err != nil {
		return err
	}
	logger.Info("static GTFS ingestion completed", "gtfs_import_id", result.ImportID,
		"status", status, "routes", result.Dataset.Summary.RouteCount,
		"stops", result.Dataset.Summary.StopCount, "route_stations", result.Dataset.Summary.RouteStationCount)
	return nil
}

func logGTFSCleanupWarning(logger *slog.Logger, result ingest.GTFSResult) {
	if result.CleanupError != nil {
		logger.Warn("static GTFS temporary cleanup failed", "gtfs_import_id", result.ImportID)
	}
}

func runIngestAlerts(ctx context.Context, args []string, getenv func(string) string, output io.Writer, logger *slog.Logger) error {
	flags := flag.NewFlagSet("ingest-alerts", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "fetch and inspect alerts without persisting them")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse ingest-alerts flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	workerConfig, err := config.LoadWorker(getenv)
	if err != nil {
		return err
	}

	client := realtime.Client{
		HTTPClient:   &http.Client{Timeout: workerConfig.HTTPTimeout},
		URL:          workerConfig.AlertsURL,
		APIKey:       workerConfig.APIKey,
		APIKeyHeader: workerConfig.APIKeyHeader,
		MaxAttempts:  3,
		RetryDelay:   time.Second,
	}
	logger.Info("fetching service alerts", "url", workerConfig.AlertsURL, "dry_run", *dryRun)
	if *dryRun {
		result, err := client.FetchAlerts(ctx)
		if err != nil {
			return err
		}
		summary, err := realtime.DecodeAlerts(result.Body)
		if err != nil {
			return err
		}
		if err := writeJSON(output, dryRunReport{
			SourceURL:   workerConfig.AlertsURL,
			RetrievedAt: result.RetrievedAt,
			HTTPStatus:  result.StatusCode,
			ContentType: result.ContentType,
			PayloadSize: len(result.Body),
			Feed:        summary,
		}); err != nil {
			return err
		}
		logger.Info(
			"service-alert dry run completed",
			"entities", summary.EntityCount,
			"alerts", summary.AlertCount,
			"payload_bytes", len(result.Body),
			"feed_timestamp", summary.Timestamp,
		)
		return nil
	}

	databaseConfig, err := config.LoadDatabase(getenv)
	if err != nil {
		return err
	}
	rawStorageConfig, err := config.LoadRawStorage(getenv)
	if err != nil {
		return err
	}
	archiveStore, err := storage.New(ctx, rawStorageConfig)
	if err != nil {
		return fmt.Errorf("configure raw storage: %w", err)
	}
	db, err := database.Open(ctx, databaseConfig.URL)
	if err != nil {
		return err
	}
	defer db.Close()

	service := ingest.AlertService{
		SourceURL: workerConfig.AlertsURL,
		Fetcher:   client,
		Store:     database.NewAlertRepository(db),
		Archive:   archiveStore,
		Decode:    realtime.DecodeAlerts,
	}
	result, err := service.Run(ctx)
	if err != nil {
		return fmt.Errorf("ingest service alerts: %w", err)
	}
	status := "succeeded"
	if result.Skipped {
		status = "skipped"
	}
	if err := writeJSON(output, ingestionReport{
		RunID:            result.RunID,
		Status:           status,
		RetrievedAt:      result.Fetch.RetrievedAt,
		PayloadSize:      len(result.Fetch.Body),
		FeedTimestamp:    result.Summary.Timestamp,
		EntityCount:      result.Summary.EntityCount,
		AlertCount:       result.Summary.AlertCount,
		ArchiveObjectKey: result.Archive.Key,
	}); err != nil {
		return err
	}
	logger.Info(
		"service-alert ingestion completed",
		"ingestion_run_id", result.RunID,
		"status", status,
		"entities", result.Summary.EntityCount,
		"alerts", result.Summary.AlertCount,
		"payload_bytes", len(result.Fetch.Body),
		"feed_timestamp", result.Summary.Timestamp,
	)
	return nil
}

func runMigrate(ctx context.Context, args []string, getenv func(string) string, logger *slog.Logger) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected migrate arguments: %v", args)
	}
	databaseConfig, err := config.LoadDatabase(getenv)
	if err != nil {
		return err
	}
	db, err := database.Open(ctx, databaseConfig.URL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		return err
	}
	logger.Info("database migrations completed")
	return nil
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write JSON report: %w", err)
	}
	return nil
}
