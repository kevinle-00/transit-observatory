package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAPIDefaults(t *testing.T) {
	config, err := LoadAPI(environment(nil))
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if config.Address != ":8080" {
		t.Errorf("Address = %q, want :8080", config.Address)
	}
	if config.CORSAllowedOrigin != "http://localhost:5173" {
		t.Errorf("CORSAllowedOrigin = %q", config.CORSAllowedOrigin)
	}
	if config.RequestTimeout != 15*time.Second {
		t.Errorf("RequestTimeout = %s, want 15s", config.RequestTimeout)
	}
	if config.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 10s", config.ShutdownTimeout)
	}
	if config.StatusAlertDataMaxAge != 10*time.Minute || config.StatusAlertCheckMaxAge != 10*time.Minute ||
		config.StatusGTFSDataMaxAge != 192*time.Hour || config.StatusGTFSCheckMaxAge != 36*time.Hour ||
		config.StatusAlertRunMaxDuration != 5*time.Minute || config.StatusGTFSRunMaxDuration != 30*time.Minute ||
		config.StatusFutureTolerance != 2*time.Minute || config.StatusRecentFailureLimit != 5 {
		t.Errorf("status defaults = %#v", config)
	}
}

func TestLoadAPIOverrides(t *testing.T) {
	config, err := LoadAPI(environment(map[string]string{
		"PORT":                      "9090",
		"CORS_ALLOWED_ORIGIN":       "https://transit.example",
		"API_REQUEST_TIMEOUT":       "2s",
		"API_SHUTDOWN_TIMEOUT":      "3s",
		"STATUS_ALERT_DATA_MAX_AGE": "1m", "STATUS_ALERT_CHECK_MAX_AGE": "2m",
		"STATUS_GTFS_DATA_MAX_AGE": "3h", "STATUS_GTFS_CHECK_MAX_AGE": "4h",
		"STATUS_ALERT_RUN_MAX_DURATION": "5m", "STATUS_GTFS_RUN_MAX_DURATION": "6m",
		"STATUS_FUTURE_TOLERANCE": "7s", "STATUS_RECENT_FAILURE_LIMIT": "8",
	}))
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if config.Address != ":9090" {
		t.Errorf("Address = %q, want :9090", config.Address)
	}
	if config.CORSAllowedOrigin != "https://transit.example" {
		t.Errorf("CORSAllowedOrigin = %q", config.CORSAllowedOrigin)
	}
	if config.RequestTimeout != 2*time.Second {
		t.Errorf("RequestTimeout = %s, want 2s", config.RequestTimeout)
	}
	if config.ShutdownTimeout != 3*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 3s", config.ShutdownTimeout)
	}
	if config.StatusAlertDataMaxAge != time.Minute || config.StatusAlertCheckMaxAge != 2*time.Minute ||
		config.StatusGTFSDataMaxAge != 3*time.Hour || config.StatusGTFSCheckMaxAge != 4*time.Hour ||
		config.StatusGTFSRunMaxDuration != 6*time.Minute || config.StatusFutureTolerance != 7*time.Second ||
		config.StatusRecentFailureLimit != 8 {
		t.Errorf("status overrides = %#v", config)
	}
}

func TestLoadAPIValidation(t *testing.T) {
	tests := []struct {
		name        string
		values      map[string]string
		wantMessage string
	}{
		{name: "zero port", values: map[string]string{"PORT": "0"}, wantMessage: "PORT must be an integer"},
		{name: "large port", values: map[string]string{"PORT": "65536"}, wantMessage: "PORT must be an integer"},
		{name: "text port", values: map[string]string{"PORT": "http"}, wantMessage: "PORT must be an integer"},
		{name: "origin path", values: map[string]string{"CORS_ALLOWED_ORIGIN": "https://example.com/app"}, wantMessage: "must not contain"},
		{name: "origin scheme", values: map[string]string{"CORS_ALLOWED_ORIGIN": "file://example.com"}, wantMessage: "must be an HTTP origin"},
		{name: "bad request timeout", values: map[string]string{"API_REQUEST_TIMEOUT": "later"}, wantMessage: "API_REQUEST_TIMEOUT must be a positive Go duration"},
		{name: "long request timeout", values: map[string]string{"API_REQUEST_TIMEOUT": "3m"}, wantMessage: "no greater than 2m0s"},
		{name: "bad timeout", values: map[string]string{"API_SHUTDOWN_TIMEOUT": "later"}, wantMessage: "must be a positive Go duration"},
		{name: "bad status duration", values: map[string]string{"STATUS_ALERT_DATA_MAX_AGE": "0s"}, wantMessage: "STATUS_ALERT_DATA_MAX_AGE"},
		{name: "large status duration", values: map[string]string{"STATUS_GTFS_CHECK_MAX_AGE": "9000h"}, wantMessage: "STATUS_GTFS_CHECK_MAX_AGE"},
		{name: "large failure limit", values: map[string]string{"STATUS_RECENT_FAILURE_LIMIT": "21"}, wantMessage: "between 1 and 20"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadAPI(environment(test.values))
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("LoadAPI() error = %v, want message containing %q", err, test.wantMessage)
			}
		})
	}
}

func TestLoadAPIAggregatesStatusValidationErrors(t *testing.T) {
	_, err := LoadAPI(environment(map[string]string{
		"STATUS_ALERT_DATA_MAX_AGE":   "bad",
		"STATUS_RECENT_FAILURE_LIMIT": "0",
	}))
	if err == nil || !strings.Contains(err.Error(), "STATUS_ALERT_DATA_MAX_AGE") || !strings.Contains(err.Error(), "STATUS_RECENT_FAILURE_LIMIT") {
		t.Fatalf("LoadAPI() error = %v", err)
	}
}

func TestLoadAPIAllowsWildcardOrigin(t *testing.T) {
	config, err := LoadAPI(environment(map[string]string{"CORS_ALLOWED_ORIGIN": "*"}))
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if config.CORSAllowedOrigin != "*" {
		t.Errorf("CORSAllowedOrigin = %q, want *", config.CORSAllowedOrigin)
	}
}
