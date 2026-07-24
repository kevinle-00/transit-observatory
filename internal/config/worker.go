package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

const (
	defaultAlertsURL   = "https://api.opendata.transport.vic.gov.au/opendata/public-transport/gtfs/realtime/v1/metro/service-alerts"
	defaultAPIHeader   = "KeyID"
	defaultHTTPTimeout = 15 * time.Second
)

type Worker struct {
	AlertsURL    string
	APIKey       string
	APIKeyHeader string
	HTTPTimeout  time.Duration
}

func LoadWorker(getenv func(string) string) (Worker, error) {
	config := Worker{
		AlertsURL:    valueOrDefault(getenv("TRANSIT_ALERTS_URL"), defaultAlertsURL),
		APIKey:       getenv("TRANSIT_API_KEY"),
		APIKeyHeader: valueOrDefault(getenv("TRANSIT_API_KEY_HEADER"), defaultAPIHeader),
		HTTPTimeout:  defaultHTTPTimeout,
	}

	var validationErrors []error
	if config.APIKey == "" {
		validationErrors = append(validationErrors, errors.New("TRANSIT_API_KEY is required"))
	}
	if !validHTTPHeaderName(config.APIKeyHeader) {
		validationErrors = append(validationErrors, fmt.Errorf("TRANSIT_API_KEY_HEADER is not a valid HTTP header name: %q", config.APIKeyHeader))
	}
	if err := validateHTTPURL(config.AlertsURL); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("TRANSIT_ALERTS_URL: %w", err))
	}

	if value := getenv("TRANSIT_HTTP_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			validationErrors = append(validationErrors, fmt.Errorf("TRANSIT_HTTP_TIMEOUT must be a positive Go duration: %q", value))
		} else {
			config.HTTPTimeout = parsed
		}
	}

	if len(validationErrors) > 0 {
		return Worker{}, fmt.Errorf("invalid worker configuration: %w", errors.Join(validationErrors...))
	}
	return config, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validateHTTPURL(value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return fmt.Errorf("must be a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("scheme must be http or https")
	}
	if parsed.Host == "" {
		return errors.New("host is required")
	}
	return nil
}

func validHTTPHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		character := value[i]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}
