package storage

import (
	"context"
	"testing"

	"github.com/kevinle-00/transit-observatory/internal/config"
)

func TestLoadS3AWSConfigCredentialChainAndStaticCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "chain-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "chain-secret")
	t.Setenv("AWS_SESSION_TOKEN", "chain-token")
	tests := []struct {
		name       string
		config     config.RawStorageS3
		wantAccess string
		wantSource string
	}{
		{name: "default chain", config: config.RawStorageS3{Region: "us-east-1"}, wantAccess: "chain-access", wantSource: "EnvConfigCredentials"},
		{name: "static", config: config.RawStorageS3{Region: "us-east-1", AccessKeyID: "static-access", SecretAccessKey: "static-secret"}, wantAccess: "static-access", wantSource: "StaticCredentials"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			awsConfig, err := loadS3AWSConfig(context.Background(), test.config)
			if err != nil {
				t.Fatal(err)
			}
			credentials, err := awsConfig.Credentials.Retrieve(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if credentials.AccessKeyID != test.wantAccess || credentials.Source != test.wantSource {
				t.Fatalf("credentials = access %q source %q", credentials.AccessKeyID, credentials.Source)
			}
		})
	}
}
