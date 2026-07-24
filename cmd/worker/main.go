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
		return errors.New("usage: worker ingest-alerts --dry-run")
	}
	if args[0] != "ingest-alerts" {
		return fmt.Errorf("unknown command %q; usage: worker ingest-alerts --dry-run", args[0])
	}

	flags := flag.NewFlagSet("ingest-alerts", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "fetch and inspect alerts without persisting them")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("parse ingest-alerts flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if !*dryRun {
		return errors.New("persistence is not implemented; run ingest-alerts with --dry-run")
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
	logger.Info("fetching service alerts", "url", workerConfig.AlertsURL, "dry_run", true)
	result, err := client.FetchAlerts(ctx)
	if err != nil {
		return err
	}
	summary, err := realtime.DecodeAlerts(result.Body)
	if err != nil {
		return err
	}

	report := dryRunReport{
		SourceURL:   workerConfig.AlertsURL,
		RetrievedAt: result.RetrievedAt,
		HTTPStatus:  result.StatusCode,
		ContentType: result.ContentType,
		PayloadSize: len(result.Body),
		Feed:        summary,
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write dry-run report: %w", err)
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
