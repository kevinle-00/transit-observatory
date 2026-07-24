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
			name: "URL credentials",
			values: map[string]string{
				"TRANSIT_API_KEY":    "secret",
				"TRANSIT_ALERTS_URL": "https://user:password@example.com/alerts",
			},
			wantMessage: "must not contain user credentials",
		},
		{
			name: "URL query parameters",
			values: map[string]string{
				"TRANSIT_API_KEY":    "secret",
				"TRANSIT_ALERTS_URL": "https://example.com/alerts?key=secret",
			},
			wantMessage: "must not contain query parameters",
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

func TestLoadDatabase(t *testing.T) {
	config, err := LoadDatabase(environment(map[string]string{
		"DATABASE_URL": "postgres://user:password@localhost:5432/transit?sslmode=disable",
	}))
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	if config.URL != "postgres://user:password@localhost:5432/transit?sslmode=disable" {
		t.Errorf("URL = %q", config.URL)
	}
}

func TestLoadDatabaseValidation(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		wantMessage string
	}{
		{name: "missing", wantMessage: "DATABASE_URL is required"},
		{name: "invalid scheme", value: "mysql://localhost/transit", wantMessage: "scheme must be postgres"},
		{name: "missing host", value: "postgres:///transit", wantMessage: "host is required"},
		{name: "missing database", value: "postgres://localhost", wantMessage: "database name is required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadDatabase(environment(map[string]string{"DATABASE_URL": test.value}))
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("LoadDatabase() error = %v, want message containing %q", err, test.wantMessage)
			}
		})
	}
}

func TestLoadDatabaseDoesNotExposeMalformedURL(t *testing.T) {
	_, err := LoadDatabase(environment(map[string]string{
		"DATABASE_URL": "postgres://user:database-secret@localhost/%zz",
	}))
	if err == nil {
		t.Fatal("LoadDatabase() error = nil")
	}
	if strings.Contains(err.Error(), "database-secret") {
		t.Fatalf("LoadDatabase() exposed database credentials: %v", err)
	}
}

func environment(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
