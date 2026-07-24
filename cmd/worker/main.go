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
	"github.com/kevinle-00/transit-observatory/internal/ingest"
	"github.com/kevinle-00/transit-observatory/internal/realtime"
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
	RunID         int64               `json:"ingestion_run_id"`
	Status        string              `json:"status"`
	RetrievedAt   time.Time           `json:"retrieved_at"`
	PayloadSize   int                 `json:"payload_size_bytes"`
	FeedTimestamp *realtime.Timestamp `json:"feed_timestamp,omitempty"`
	EntityCount   int                 `json:"entity_count"`
	AlertCount    int                 `json:"alert_count"`
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
		return errors.New("usage: worker <ingest-alerts|migrate> [options]")
	}
	switch args[0] {
	case "ingest-alerts":
		return runIngestAlerts(ctx, args[1:], getenv, output, logger)
	case "migrate":
		return runMigrate(ctx, args[1:], getenv, logger)
	default:
		return fmt.Errorf("unknown command %q; usage: worker <ingest-alerts|migrate> [options]", args[0])
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
	db, err := database.Open(ctx, databaseConfig.URL)
	if err != nil {
		return err
	}
	defer db.Close()

	service := ingest.AlertService{
		SourceURL: workerConfig.AlertsURL,
		Fetcher:   client,
		Store:     database.NewAlertRepository(db),
		Decode:    realtime.DecodeAlerts,
	}
	result, err := service.Run(ctx)
	if err != nil {
		return fmt.Errorf("ingest service alerts: %w", err)
	}
	if err := writeJSON(output, ingestionReport{
		RunID:         result.RunID,
		Status:        "succeeded",
		RetrievedAt:   result.Fetch.RetrievedAt,
		PayloadSize:   len(result.Fetch.Body),
		FeedTimestamp: result.Summary.Timestamp,
		EntityCount:   result.Summary.EntityCount,
		AlertCount:    result.Summary.AlertCount,
	}); err != nil {
		return err
	}
	logger.Info(
		"service-alert ingestion completed",
		"ingestion_run_id", result.RunID,
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
