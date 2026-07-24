package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/realtime"
)

type AlertFetcher interface {
	FetchAlerts(context.Context) (realtime.FetchResult, error)
}

type AlertStore interface {
	StartAlertRun(context.Context, string) (int64, error)
	FailAlertRun(context.Context, int64, *realtime.FetchResult, *realtime.FeedSummary, error) error
	CompleteAlertRun(context.Context, int64, realtime.FetchResult, realtime.FeedSummary) error
}

type AlertResult struct {
	RunID   int64
	Fetch   realtime.FetchResult
	Summary realtime.FeedSummary
}

type AlertService struct {
	SourceURL string
	Fetcher   AlertFetcher
	Store     AlertStore
	Decode    func([]byte) (realtime.FeedSummary, error)
}

func (s AlertService) Run(ctx context.Context) (AlertResult, error) {
	runID, err := s.Store.StartAlertRun(ctx, s.SourceURL)
	if err != nil {
		return AlertResult{}, err
	}

	fetch, err := s.Fetcher.FetchAlerts(ctx)
	if err != nil {
		return AlertResult{RunID: runID}, s.fail(ctx, runID, nil, nil, err)
	}
	summary, err := s.Decode(fetch.Body)
	if err != nil {
		return AlertResult{RunID: runID, Fetch: fetch}, s.fail(ctx, runID, &fetch, nil, err)
	}
	if err := s.Store.CompleteAlertRun(ctx, runID, fetch, summary); err != nil {
		if commitOutcomeUnknown(err) {
			return AlertResult{RunID: runID, Fetch: fetch, Summary: summary}, err
		}
		return AlertResult{RunID: runID, Fetch: fetch, Summary: summary}, s.fail(ctx, runID, &fetch, &summary, err)
	}
	return AlertResult{RunID: runID, Fetch: fetch, Summary: summary}, nil
}

func (s AlertService) fail(
	ctx context.Context,
	runID int64,
	fetch *realtime.FetchResult,
	summary *realtime.FeedSummary,
	ingestionError error,
) error {
	failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.Store.FailAlertRun(failureContext, runID, fetch, summary, ingestionError); err != nil {
		return errors.Join(ingestionError, fmt.Errorf("record ingestion failure: %w", err))
	}
	return ingestionError
}

func commitOutcomeUnknown(err error) bool {
	var unknown interface{ CommitOutcomeUnknown() bool }
	return errors.As(err, &unknown) && unknown.CommitOutcomeUnknown()
}
