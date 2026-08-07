package realtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientFetchAlerts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Test-Key"); got != "secret" {
			t.Errorf("API key header = %q, want secret", got)
		}
		if !strings.Contains(r.Header.Get("Accept"), "application/x-protobuf") {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	defer server.Close()

	client := Client{
		HTTPClient:   server.Client(),
		URL:          server.URL,
		APIKey:       "secret",
		APIKeyHeader: "Test-Key",
	}
	result, err := client.FetchAlerts(context.Background())
	if err != nil {
		t.Fatalf("FetchAlerts() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || len(result.Body) != 3 {
		t.Errorf("result = %#v", result)
	}
}

func TestClientFetchAlertsRejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"not protobuf"}`))
	}))
	defer server.Close()

	client := Client{HTTPClient: server.Client(), URL: server.URL, APIKey: "secret", APIKeyHeader: "Test-Key"}
	_, err := client.FetchAlerts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected service-alert content type") {
		t.Fatalf("FetchAlerts() error = %v, want content-type error", err)
	}
}

func TestClientFetchAlertsIncludesHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid subscription key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := Client{HTTPClient: server.Client(), URL: server.URL, APIKey: "wrong", APIKeyHeader: "Test-Key"}
	_, err := client.FetchAlerts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 401: invalid subscription key") {
		t.Fatalf("FetchAlerts() error = %v, want HTTP status and response", err)
	}
}

func TestClientFetchAlertsRetriesTransientTransportFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	defer server.Close()

	client := Client{
		HTTPClient: server.Client(), URL: server.URL, APIKey: "secret", APIKeyHeader: "Test-Key",
		MaxAttempts: 3,
	}
	result, err := client.FetchAlerts(context.Background())
	if err != nil {
		t.Fatalf("FetchAlerts() error = %v", err)
	}
	if attempts != 3 || len(result.Body) != 3 {
		t.Fatalf("attempts/result = %d/%#v, want 3 attempts and successful result", attempts, result)
	}
}

func TestClientFetchAlertsRetriesRequestTimeout(t *testing.T) {
	attempts := 0
	client := Client{
		HTTPClient: httpClientFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/x-protobuf"}},
				Body:       io.NopCloser(strings.NewReader("protobuf")),
			}, nil
		}),
		URL: "https://example.test/alerts", APIKey: "secret", APIKeyHeader: "Test-Key", MaxAttempts: 2,
	}

	result, err := client.FetchAlerts(context.Background())
	if err != nil {
		t.Fatalf("FetchAlerts() error = %v", err)
	}
	if attempts != 2 || string(result.Body) != "protobuf" {
		t.Fatalf("attempts/body = %d/%q, want successful second attempt", attempts, result.Body)
	}
}

func TestClientFetchAlertsDoesNotRetryPermanentFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "invalid subscription key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := Client{
		HTTPClient: server.Client(), URL: server.URL, APIKey: "wrong", APIKeyHeader: "Test-Key",
		MaxAttempts: 3,
	}
	_, err := client.FetchAlerts(context.Background())
	if err == nil || attempts != 1 {
		t.Fatalf("FetchAlerts() error/attempts = %v/%d, want permanent failure after one attempt", err, attempts)
	}
}

type httpClientFunc func(*http.Request) (*http.Response, error)

func (f httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}
