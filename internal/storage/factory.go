package storage

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/kevinle-00/transit-observatory/internal/config"
)

func New(ctx context.Context, storageConfig config.RawStorage) (Store, error) {
	switch storageConfig.Backend {
	case "local":
		return NewLocalStore(storageConfig.LocalDir)
	case "s3":
		awsConfig, err := loadS3AWSConfig(ctx, storageConfig.S3)
		if err != nil {
			return nil, errors.New("load S3 storage configuration failed")
		}
		client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
			options.UsePathStyle = storageConfig.S3.PathStyle
			if storageConfig.S3.Endpoint != "" {
				options.BaseEndpoint = aws.String(storageConfig.S3.Endpoint)
			}
		})
		return newS3Store(client, storageConfig.S3.Bucket), nil
	default:
		return nil, errors.New("unsupported raw storage backend")
	}
}

func loadS3AWSConfig(ctx context.Context, storageConfig config.RawStorageS3) (aws.Config, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(storageConfig.Region)}
	if storageConfig.AccessKeyID != "" {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			storageConfig.AccessKeyID, storageConfig.SecretAccessKey, storageConfig.SessionToken)))
	}
	if storageConfig.Endpoint != "" {
		options = append(options, awsconfig.WithBaseEndpoint(storageConfig.Endpoint))
	}
	return awsconfig.LoadDefaultConfig(ctx, options...)
}
