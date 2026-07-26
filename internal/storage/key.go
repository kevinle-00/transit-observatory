package storage

import (
	"errors"
	"fmt"
	"time"
)

func ServiceAlertsKey(retrievedAt time.Time, sha256 string) (string, error) {
	if err := validateKeyInputs(retrievedAt, sha256); err != nil {
		return "", err
	}
	retrievedAt = retrievedAt.UTC()
	return fmt.Sprintf("raw/service-alerts/%s/%s-%s.pb",
		retrievedAt.Format("2006/01/02/15"), retrievedAt.Format("20060102T150405.000000000Z"), sha256), nil
}

func GTFSKey(retrievedAt time.Time, sha256 string) (string, error) {
	if err := validateKeyInputs(retrievedAt, sha256); err != nil {
		return "", err
	}
	retrievedAt = retrievedAt.UTC()
	return fmt.Sprintf("raw/gtfs/%s/%s-%s.zip",
		retrievedAt.Format("2006/01/02"), retrievedAt.Format("20060102T150405.000000000Z"), sha256), nil
}

func validateKeyInputs(retrievedAt time.Time, sha256 string) error {
	if retrievedAt.IsZero() {
		return errors.New("retrieval time is required")
	}
	if !hashPattern.MatchString(sha256) {
		return errors.New("SHA256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}
