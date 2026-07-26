package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
)

const (
	defaultRawStorageBackend = "local"
	defaultRawStorageDir     = "./var"
)

type RawStorage struct {
	Backend  string
	LocalDir string
	S3       RawStorageS3
}

type RawStorageS3 struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	PathStyle       bool
}

func LoadRawStorage(getenv func(string) string) (RawStorage, error) {
	config := RawStorage{
		Backend:  valueOrDefault(getenv("RAW_STORAGE_BACKEND"), defaultRawStorageBackend),
		LocalDir: valueOrDefault(getenv("RAW_STORAGE_LOCAL_DIR"), defaultRawStorageDir),
		S3: RawStorageS3{
			Bucket:          getenv("RAW_STORAGE_S3_BUCKET"),
			Region:          getenv("RAW_STORAGE_S3_REGION"),
			Endpoint:        getenv("RAW_STORAGE_S3_ENDPOINT"),
			AccessKeyID:     getenv("RAW_STORAGE_S3_ACCESS_KEY_ID"),
			SecretAccessKey: getenv("RAW_STORAGE_S3_SECRET_ACCESS_KEY"),
			SessionToken:    getenv("RAW_STORAGE_S3_SESSION_TOKEN"),
			PathStyle:       true,
		},
	}

	var validationErrors []error
	switch config.Backend {
	case "local":
		if config.LocalDir == "" {
			validationErrors = append(validationErrors, errors.New("RAW_STORAGE_LOCAL_DIR is required for local storage"))
		} else {
			absolute, err := filepath.Abs(config.LocalDir)
			if err != nil {
				validationErrors = append(validationErrors, errors.New("RAW_STORAGE_LOCAL_DIR cannot be normalized"))
			} else {
				config.LocalDir = filepath.Clean(absolute)
			}
		}
	case "s3":
		if value := getenv("RAW_STORAGE_S3_PATH_STYLE"); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil || (value != "true" && value != "false") {
				validationErrors = append(validationErrors, errors.New("RAW_STORAGE_S3_PATH_STYLE must be true or false"))
			} else {
				config.S3.PathStyle = parsed
			}
		}
		for _, required := range []struct {
			name  string
			value string
		}{
			{"RAW_STORAGE_S3_BUCKET", config.S3.Bucket},
			{"RAW_STORAGE_S3_REGION", config.S3.Region},
		} {
			if required.value == "" {
				validationErrors = append(validationErrors, fmt.Errorf("%s is required for S3 storage", required.name))
			}
		}
		if (config.S3.AccessKeyID == "") != (config.S3.SecretAccessKey == "") {
			validationErrors = append(validationErrors, errors.New("RAW_STORAGE_S3_ACCESS_KEY_ID and RAW_STORAGE_S3_SECRET_ACCESS_KEY must be provided together"))
		}
		if config.S3.SessionToken != "" && config.S3.AccessKeyID == "" {
			validationErrors = append(validationErrors, errors.New("RAW_STORAGE_S3_SESSION_TOKEN requires static access key and secret credentials"))
		}
		if config.S3.Endpoint != "" {
			if err := validateStorageEndpoint(config.S3.Endpoint); err != nil {
				validationErrors = append(validationErrors, fmt.Errorf("RAW_STORAGE_S3_ENDPOINT: %w", err))
			}
		}
	default:
		validationErrors = append(validationErrors, fmt.Errorf("RAW_STORAGE_BACKEND must be local or s3, got %q", config.Backend))
	}

	if len(validationErrors) > 0 {
		return RawStorage{}, fmt.Errorf("invalid raw storage configuration: %w", errors.Join(validationErrors...))
	}
	return config, nil
}

func validateStorageEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return errors.New("must be a valid HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("scheme must be http or https")
	}
	if parsed.Host == "" {
		return errors.New("host is required")
	}
	if parsed.User != nil {
		return errors.New("must not contain user credentials")
	}
	if parsed.RawQuery != "" {
		return errors.New("must not contain query parameters")
	}
	if parsed.Fragment != "" {
		return errors.New("must not contain a fragment")
	}
	return nil
}
