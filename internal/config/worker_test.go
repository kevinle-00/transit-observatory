package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWorkerDefaults(t *testing.T) {
	config, err := LoadWorker(environment(map[string]string{
		"TRANSIT_API_KEY": "secret",
	}))
	if err != nil {
		t.Fatalf("LoadWorker() error = %v", err)
	}

	if config.AlertsURL != defaultAlertsURL {
		t.Errorf("AlertsURL = %q, want %q", config.AlertsURL, defaultAlertsURL)
	}
	if config.APIKeyHeader != "KeyID" {
		t.Errorf("APIKeyHeader = %q, want KeyID", config.APIKeyHeader)
	}
	if config.HTTPTimeout != 15*time.Second {
		t.Errorf("HTTPTimeout = %s, want 15s", config.HTTPTimeout)
	}
}

func TestLoadWorkerOverrides(t *testing.T) {
	config, err := LoadWorker(environment(map[string]string{
		"TRANSIT_API_KEY":        "secret",
		"TRANSIT_API_KEY_HEADER": "Subscription-Key",
		"TRANSIT_ALERTS_URL":     "http://localhost:8080/alerts",
		"TRANSIT_HTTP_TIMEOUT":   "2.5s",
	}))
	if err != nil {
		t.Fatalf("LoadWorker() error = %v", err)
	}

	if config.AlertsURL != "http://localhost:8080/alerts" {
		t.Errorf("AlertsURL = %q", config.AlertsURL)
	}
	if config.APIKeyHeader != "Subscription-Key" {
		t.Errorf("APIKeyHeader = %q", config.APIKeyHeader)
	}
	if config.HTTPTimeout != 2500*time.Millisecond {
		t.Errorf("HTTPTimeout = %s, want 2.5s", config.HTTPTimeout)
	}
}

func TestLoadWorkerValidation(t *testing.T) {
	tests := []struct {
		name        string
		values      map[string]string
		wantMessage string
	}{
		{name: "missing API key", values: map[string]string{}, wantMessage: "TRANSIT_API_KEY is required"},
		{
			name: "invalid header",
			values: map[string]string{
				"TRANSIT_API_KEY":        "secret",
				"TRANSIT_API_KEY_HEADER": "Invalid Header",
			},
			wantMessage: "not a valid HTTP header name",
		},
		{
			name: "invalid URL scheme",
			values: map[string]string{
				"TRANSIT_API_KEY":    "secret",
				"TRANSIT_ALERTS_URL": "ftp://example.com/alerts",
			},
			wantMessage: "scheme must be http or https",
		},
		{
			name: "URL without host",
			values: map[string]string{
				"TRANSIT_API_KEY":    "secret",
				"TRANSIT_ALERTS_URL": "https:///alerts",
			},
			wantMessage: "host is required",
		},
		{
			name: "invalid timeout",
			values: map[string]string{
				"TRANSIT_API_KEY":      "secret",
				"TRANSIT_HTTP_TIMEOUT": "eventually",
			},
			wantMessage: "must be a positive Go duration",
		},
		{
			name: "zero timeout",
			values: map[string]string{
				"TRANSIT_API_KEY":      "secret",
				"TRANSIT_HTTP_TIMEOUT": "0s",
			},
			wantMessage: "must be a positive Go duration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadWorker(environment(test.values))
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("LoadWorker() error = %v, want message containing %q", err, test.wantMessage)
			}
		})
	}
}

func TestLoadWorkerReportsAllInvalidValues(t *testing.T) {
	_, err := LoadWorker(environment(map[string]string{
		"TRANSIT_API_KEY_HEADER": "bad header",
		"TRANSIT_ALERTS_URL":     "not-a-url",
		"TRANSIT_HTTP_TIMEOUT":   "-1s",
	}))
	if err == nil {
		t.Fatal("LoadWorker() error = nil, want validation errors")
	}
	for _, message := range []string{"TRANSIT_API_KEY", "TRANSIT_API_KEY_HEADER", "TRANSIT_ALERTS_URL", "TRANSIT_HTTP_TIMEOUT"} {
		if !strings.Contains(err.Error(), message) {
			t.Errorf("LoadWorker() error = %q, want %q", err, message)
		}
	}
}

func environment(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
