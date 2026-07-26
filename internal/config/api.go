package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultAPIPort           = "8080"
	defaultCORSAllowedOrigin = "http://localhost:5173"
	defaultAPIRequestTimeout = 15 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	maxAPIRequestTimeout     = 2 * time.Minute
	maxStatusAge             = 365 * 24 * time.Hour
	maxStatusRunDuration     = 24 * time.Hour
	maxStatusFutureTolerance = time.Hour
	maxRecentFailureLimit    = 20
)

type API struct {
	Address                   string
	CORSAllowedOrigin         string
	RequestTimeout            time.Duration
	ShutdownTimeout           time.Duration
	StatusAlertDataMaxAge     time.Duration
	StatusAlertCheckMaxAge    time.Duration
	StatusGTFSDataMaxAge      time.Duration
	StatusGTFSCheckMaxAge     time.Duration
	StatusAlertRunMaxDuration time.Duration
	StatusGTFSRunMaxDuration  time.Duration
	StatusFutureTolerance     time.Duration
	StatusRecentFailureLimit  int
}

func LoadAPI(getenv func(string) string) (API, error) {
	port := valueOrDefault(getenv("PORT"), defaultAPIPort)
	config := API{
		Address:               net.JoinHostPort("", port),
		CORSAllowedOrigin:     valueOrDefault(getenv("CORS_ALLOWED_ORIGIN"), defaultCORSAllowedOrigin),
		RequestTimeout:        defaultAPIRequestTimeout,
		ShutdownTimeout:       defaultShutdownTimeout,
		StatusAlertDataMaxAge: 10 * time.Minute, StatusAlertCheckMaxAge: 10 * time.Minute,
		StatusGTFSDataMaxAge: 192 * time.Hour, StatusGTFSCheckMaxAge: 36 * time.Hour,
		StatusAlertRunMaxDuration: 5 * time.Minute, StatusGTFSRunMaxDuration: 30 * time.Minute,
		StatusFutureTolerance: 2 * time.Minute, StatusRecentFailureLimit: 5,
	}
	var validationErrors []error
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		validationErrors = append(validationErrors, fmt.Errorf("PORT must be an integer between 1 and 65535: %q", port))
	}
	if err := validateOrigin(config.CORSAllowedOrigin); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("CORS_ALLOWED_ORIGIN: %w", err))
	}
	if value := getenv("API_REQUEST_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 || parsed > maxAPIRequestTimeout {
			validationErrors = append(validationErrors, fmt.Errorf("API_REQUEST_TIMEOUT must be a positive Go duration no greater than %s: %q", maxAPIRequestTimeout, value))
		} else {
			config.RequestTimeout = parsed
		}
	}
	if value := getenv("API_SHUTDOWN_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			validationErrors = append(validationErrors, fmt.Errorf("API_SHUTDOWN_TIMEOUT must be a positive Go duration: %q", value))
		} else {
			config.ShutdownTimeout = parsed
		}
	}
	durations := []struct {
		name   string
		target *time.Duration
		max    time.Duration
	}{
		{"STATUS_ALERT_DATA_MAX_AGE", &config.StatusAlertDataMaxAge, maxStatusAge},
		{"STATUS_ALERT_CHECK_MAX_AGE", &config.StatusAlertCheckMaxAge, maxStatusAge},
		{"STATUS_GTFS_DATA_MAX_AGE", &config.StatusGTFSDataMaxAge, maxStatusAge},
		{"STATUS_GTFS_CHECK_MAX_AGE", &config.StatusGTFSCheckMaxAge, maxStatusAge},
		{"STATUS_ALERT_RUN_MAX_DURATION", &config.StatusAlertRunMaxDuration, maxStatusRunDuration},
		{"STATUS_GTFS_RUN_MAX_DURATION", &config.StatusGTFSRunMaxDuration, maxStatusRunDuration},
		{"STATUS_FUTURE_TOLERANCE", &config.StatusFutureTolerance, maxStatusFutureTolerance},
	}
	for _, item := range durations {
		if value := getenv(item.name); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil || parsed <= 0 || parsed > item.max {
				validationErrors = append(validationErrors, fmt.Errorf("%s must be a positive Go duration no greater than %s: %q", item.name, item.max, value))
			} else {
				*item.target = parsed
			}
		}
	}
	if value := getenv("STATUS_RECENT_FAILURE_LIMIT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 || parsed > maxRecentFailureLimit {
			validationErrors = append(validationErrors, fmt.Errorf("STATUS_RECENT_FAILURE_LIMIT must be an integer between 1 and %d: %q", maxRecentFailureLimit, value))
		} else {
			config.StatusRecentFailureLimit = parsed
		}
	}
	if len(validationErrors) > 0 {
		return API{}, fmt.Errorf("invalid API configuration: %w", errors.Join(validationErrors...))
	}
	return config, nil
}

func validateOrigin(value string) error {
	if value == "*" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return errors.New("must be an HTTP origin or *")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("must be an HTTP origin or *")
	}
	if parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must not contain credentials, a path, query parameters, or a fragment")
	}
	return nil
}
