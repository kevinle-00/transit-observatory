package realtime

import (
	"context"
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
