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
)

type API struct {
	Address           string
	CORSAllowedOrigin string
	RequestTimeout    time.Duration
	ShutdownTimeout   time.Duration
}

func LoadAPI(getenv func(string) string) (API, error) {
	port := valueOrDefault(getenv("PORT"), defaultAPIPort)
	config := API{
		Address:           net.JoinHostPort("", port),
		CORSAllowedOrigin: valueOrDefault(getenv("CORS_ALLOWED_ORIGIN"), defaultCORSAllowedOrigin),
		RequestTimeout:    defaultAPIRequestTimeout,
		ShutdownTimeout:   defaultShutdownTimeout,
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
