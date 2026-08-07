package realtime

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

const (
	maxPayloadBytes = 32 << 20
	maxErrorBytes   = 4 << 10
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	HTTPClient   HTTPClient
	URL          string
	APIKey       string
	APIKeyHeader string
	MaxAttempts  int
	RetryDelay   time.Duration
}

type FetchResult struct {
	Body        []byte
	StatusCode  int
	ContentType string
	RetrievedAt time.Time
}

func (c Client) FetchAlerts(ctx context.Context) (FetchResult, error) {
	if c.HTTPClient == nil {
		return FetchResult{}, fmt.Errorf("HTTP client is required")
	}
	if c.URL == "" {
		return FetchResult{}, fmt.Errorf("alerts URL is required")
	}
	if c.APIKey == "" {
		return FetchResult{}, fmt.Errorf("API key is required")
	}
	if c.APIKeyHeader == "" {
		return FetchResult{}, fmt.Errorf("API key header is required")
	}
	attempts := c.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		result, retryable, err := c.fetchAlertsOnce(ctx)
		if err == nil || !retryable || attempt == attempts || ctx.Err() != nil {
			return result, err
		}
		if err := waitForRetry(ctx, c.RetryDelay*time.Duration(1<<(attempt-1))); err != nil {
			return FetchResult{}, fmt.Errorf("fetch service alerts: %w", err)
		}
	}
	panic("unreachable")
}

func (c Client) fetchAlertsOnce(ctx context.Context) (FetchResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return FetchResult{}, false, fmt.Errorf("create service-alert request: %w", err)
	}
	req.Header.Set("Accept", "application/x-protobuf, application/protobuf, application/octet-stream")
	req.Header.Set(c.APIKeyHeader, c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return FetchResult{}, ctx.Err() == nil, fmt.Errorf("fetch service alerts: %w", err)
	}
	defer resp.Body.Close()

	retrievedAt := time.Now().UTC()
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
		return FetchResult{}, retryableHTTPStatus(resp.StatusCode), fmt.Errorf("fetch service alerts: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := validateProtobufContentType(contentType); err != nil {
		return FetchResult{}, false, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPayloadBytes+1))
	if err != nil {
		return FetchResult{}, false, fmt.Errorf("read service-alert response: %w", err)
	}
	if len(body) > maxPayloadBytes {
		return FetchResult{}, false, fmt.Errorf("service-alert response exceeds %d bytes", maxPayloadBytes)
	}
	if len(body) == 0 {
		return FetchResult{}, false, fmt.Errorf("service-alert response is empty")
	}

	return FetchResult{
		Body:        body,
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		RetrievedAt: retrievedAt,
	}, false, nil
}

func retryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validateProtobufContentType(value string) error {
	if value == "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("invalid service-alert content type %q: %w", value, err)
	}
	switch mediaType {
	case "application/octet-stream", "application/protobuf", "application/x-protobuf", "application/vnd.google.protobuf":
		return nil
	default:
		return fmt.Errorf("unexpected service-alert content type %q", value)
	}
}
