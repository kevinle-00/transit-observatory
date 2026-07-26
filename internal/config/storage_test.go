package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRawStorageDefaults(t *testing.T) {
	config, err := LoadRawStorage(environment(nil))
	if err != nil {
		t.Fatalf("LoadRawStorage() error = %v", err)
	}
	wantDir, _ := filepath.Abs("./var")
	if config.Backend != "local" || config.LocalDir != wantDir || !config.S3.PathStyle {
		t.Fatalf("LoadRawStorage() = %+v", config)
	}
}

func TestLoadRawStorageS3(t *testing.T) {
	config, err := LoadRawStorage(environment(map[string]string{
		"RAW_STORAGE_BACKEND":              "s3",
		"RAW_STORAGE_S3_BUCKET":            "raw-archive",
		"RAW_STORAGE_S3_REGION":            "us-east-1",
		"RAW_STORAGE_S3_ENDPOINT":          "https://objects.example.test:9443/base",
		"RAW_STORAGE_S3_ACCESS_KEY_ID":     "access-value",
		"RAW_STORAGE_S3_SECRET_ACCESS_KEY": "secret-value",
		"RAW_STORAGE_S3_SESSION_TOKEN":     "token-value",
		"RAW_STORAGE_S3_PATH_STYLE":        "false",
	}))
	if err != nil {
		t.Fatalf("LoadRawStorage() error = %v", err)
	}
	if config.S3.Bucket != "raw-archive" || config.S3.Region != "us-east-1" || config.S3.PathStyle {
		t.Fatalf("LoadRawStorage() = %+v", config)
	}
}

func TestLoadRawStorageS3DefaultCredentialChain(t *testing.T) {
	config, err := LoadRawStorage(environment(map[string]string{
		"RAW_STORAGE_BACKEND":   "s3",
		"RAW_STORAGE_S3_BUCKET": "raw-archive",
		"RAW_STORAGE_S3_REGION": "us-east-1",
	}))
	if err != nil {
		t.Fatalf("LoadRawStorage() error = %v", err)
	}
	if config.S3.AccessKeyID != "" || config.S3.SecretAccessKey != "" || config.S3.SessionToken != "" {
		t.Fatalf("static credentials unexpectedly populated: %+v", config.S3)
	}
}

func TestLoadRawStorageS3CredentialPairing(t *testing.T) {
	for _, credentials := range map[string]map[string]string{
		"access only": {"RAW_STORAGE_S3_ACCESS_KEY_ID": "sensitive-access"},
		"secret only": {"RAW_STORAGE_S3_SECRET_ACCESS_KEY": "sensitive-secret"},
		"token only":  {"RAW_STORAGE_S3_SESSION_TOKEN": "sensitive-token"},
	} {
		credentials["RAW_STORAGE_BACKEND"] = "s3"
		credentials["RAW_STORAGE_S3_BUCKET"] = "raw-archive"
		credentials["RAW_STORAGE_S3_REGION"] = "us-east-1"
		_, err := LoadRawStorage(environment(credentials))
		if err == nil {
			t.Fatalf("LoadRawStorage(%v) error = nil", credentials)
		}
		for _, secret := range []string{"sensitive-access", "sensitive-secret", "sensitive-token"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("LoadRawStorage() exposed credential: %v", err)
			}
		}
	}
}

func TestLoadRawStorageReportsAllS3ErrorsWithoutSecrets(t *testing.T) {
	access := "sensitive-access-value"
	secret := "sensitive-secret-value"
	_, err := LoadRawStorage(environment(map[string]string{
		"RAW_STORAGE_BACKEND":              "s3",
		"RAW_STORAGE_S3_ENDPOINT":          "https://user:password@example.test/path?token=hidden#fragment",
		"RAW_STORAGE_S3_ACCESS_KEY_ID":     access,
		"RAW_STORAGE_S3_SECRET_ACCESS_KEY": secret,
		"RAW_STORAGE_S3_PATH_STYLE":        "yes",
	}))
	if err == nil {
		t.Fatal("LoadRawStorage() error = nil")
	}
	for _, name := range []string{"RAW_STORAGE_S3_BUCKET", "RAW_STORAGE_S3_REGION", "RAW_STORAGE_S3_ENDPOINT", "RAW_STORAGE_S3_PATH_STYLE"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not contain %s", err, name)
		}
	}
	for _, value := range []string{access, secret, "password", "hidden"} {
		if strings.Contains(err.Error(), value) {
			t.Errorf("error exposed sensitive value %q: %v", value, err)
		}
	}
}

func TestLoadRawStorageValidation(t *testing.T) {
	tests := []map[string]string{
		{"RAW_STORAGE_BACKEND": "other"},
		{"RAW_STORAGE_BACKEND": "s3", "RAW_STORAGE_S3_ENDPOINT": "ftp://example.test"},
		{"RAW_STORAGE_BACKEND": "s3", "RAW_STORAGE_S3_ENDPOINT": "https:///objects"},
	}
	for _, values := range tests {
		if _, err := LoadRawStorage(environment(values)); err == nil {
			t.Errorf("LoadRawStorage(%v) error = nil", values)
		}
	}
}
