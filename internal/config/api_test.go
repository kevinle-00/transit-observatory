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
}

func TestLoadAPIOverrides(t *testing.T) {
	config, err := LoadAPI(environment(map[string]string{
		"PORT":                 "9090",
		"CORS_ALLOWED_ORIGIN":  "https://transit.example",
		"API_REQUEST_TIMEOUT":  "2s",
		"API_SHUTDOWN_TIMEOUT": "3s",
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

func TestLoadAPIAllowsWildcardOrigin(t *testing.T) {
	config, err := LoadAPI(environment(map[string]string{"CORS_ALLOWED_ORIGIN": "*"}))
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if config.CORSAllowedOrigin != "*" {
		t.Errorf("CORSAllowedOrigin = %q, want *", config.CORSAllowedOrigin)
	}
}
