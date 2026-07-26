package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/observability"
	"github.com/kevinle-00/transit-observatory/internal/realtime"
	"github.com/kevinle-00/transit-observatory/internal/storage"
)

type AlertFetcher interface {
	FetchAlerts(context.Context) (realtime.FetchResult, error)
}

type AlertStore interface {
	StartAlertRun(context.Context, string) (int64, error)
	RecordAlertArchive(context.Context, int64, storage.Object) error
	FailAlertRunWithFailure(context.Context, int64, *realtime.FetchResult, *realtime.FeedSummary, observability.Failure) error
	CompleteAlertRun(context.Context, int64, realtime.FetchResult, realtime.FeedSummary) (bool, error)
}

type AlertResult struct {
	RunID   int64
	Fetch   realtime.FetchResult
	Summary realtime.FeedSummary
	Archive storage.Object
	Skipped bool
}

type AlertService struct {
	SourceURL string
	Fetcher   AlertFetcher
	Store     AlertStore
	Archive   storage.Store
	Decode    func([]byte) (realtime.FeedSummary, error)
}

func (s AlertService) Run(ctx context.Context) (AlertResult, error) {
	runID, err := s.Store.StartAlertRun(ctx, s.SourceURL)
	if err != nil {
		return AlertResult{}, err
	}

	fetch, err := s.Fetcher.FetchAlerts(ctx)
	if err != nil {
		return AlertResult{RunID: runID}, s.fail(ctx, runID, nil, nil, failure("fetch", "fetch_failed", "Service-alert fetch failed", err))
	}
	hashBytes := sha256.Sum256(fetch.Body)
	hash := hex.EncodeToString(hashBytes[:])
	key, err := storage.ServiceAlertsKey(fetch.RetrievedAt, hash)
	if err != nil {
		return AlertResult{RunID: runID, Fetch: fetch}, s.fail(ctx, runID, &fetch, nil, failure("archive", "archive_failed", "Service-alert archive failed", err))
	}
	archive, err := s.Archive.Put(ctx, storage.PutRequest{
		Key: key, Source: storage.BytesSource(fetch.Body), Size: int64(len(fetch.Body)), SHA256: hash, ContentType: fetch.ContentType,
	})
	if err != nil {
		return AlertResult{RunID: runID, Fetch: fetch}, s.fail(ctx, runID, &fetch, nil, failure("archive", "archive_failed", "Service-alert archive failed", err))
	}
	result := AlertResult{RunID: runID, Fetch: fetch, Archive: archive}
	if err := s.Store.RecordAlertArchive(ctx, runID, archive); err != nil {
		if commitOutcomeUnknown(err) {
			return result, err
		}
		return result, s.fail(ctx, runID, &fetch, nil, failure("archive", "archive_failed", "Service-alert archive failed", err))
	}
	summary, err := s.Decode(fetch.Body)
	if err != nil {
		return result, s.fail(ctx, runID, &fetch, nil, failure("decode", "decode_failed", "Service-alert decode failed", err))
	}
	result.Summary = summary
	skipped, err := s.Store.CompleteAlertRun(ctx, runID, fetch, summary)
	if err != nil {
		if commitOutcomeUnknown(err) {
			return result, err
		}
		return result, s.fail(ctx, runID, &fetch, &summary, failure("persist", "persist_failed", "Service-alert persistence failed", err))
	}
	result.Skipped = skipped
	return result, nil
}

func (s AlertService) fail(
	ctx context.Context,
	runID int64,
	fetch *realtime.FetchResult,
	summary *realtime.FeedSummary,
	failure observability.Failure,
) error {
	failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.Store.FailAlertRunWithFailure(failureContext, runID, fetch, summary, failure); err != nil {
		return errors.Join(failure, fmt.Errorf("record ingestion failure: %w", err))
	}
	return failure
}

func failure(stage, code, publicMessage string, err error) observability.Failure {
	return observability.Failure{Stage: stage, Code: code, PublicMessage: publicMessage, Err: err}
}

func commitOutcomeUnknown(err error) bool {
	var unknown interface{ CommitOutcomeUnknown() bool }
	return errors.As(err, &unknown) && unknown.CommitOutcomeUnknown()
}
