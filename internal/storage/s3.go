package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	sha256MetadataKey = "sha256"
	maxPutAttempts    = 3
)

type s3Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type S3Store struct {
	client s3Client
	bucket string
}

func newS3Store(client s3Client, bucket string) *S3Store {
	return &S3Store{client: client, bucket: bucket}
}

func (store *S3Store) Put(ctx context.Context, request PutRequest) (Object, error) {
	if err := validateRequest(request); err != nil {
		return Object{}, err
	}
	for attempt := 0; attempt < maxPutAttempts; attempt++ {
		output, err := store.putOnce(ctx, request)
		if err == nil {
			return newObject("s3", request, time.Now().UTC(), aws.ToString(output.ETag), aws.ToString(output.VersionId), Created(true)), nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Object{}, ctxErr
		}
		switch classifyS3PutError(err) {
		case s3ErrorPrecondition:
			return store.verifyExisting(ctx, request, Created(false))
		case s3ErrorConditionalConflict:
			if attempt+1 < maxPutAttempts {
				if err := waitForS3Retry(ctx, attempt); err != nil {
					return Object{}, err
				}
				continue
			}
			return Object{}, errors.New("S3 object create failed with repeated conditional conflicts")
		case s3ErrorAmbiguous:
			return store.verifyExisting(ctx, request, nil)
		default:
			return Object{}, errors.New("S3 object create failed")
		}
	}
	return Object{}, errors.New("S3 object create failed")
}

func (store *S3Store) putOnce(ctx context.Context, request PutRequest) (*s3.PutObjectOutput, error) {
	source, err := request.Source.Open()
	if err != nil {
		return nil, errors.New("open storage source failed")
	}
	defer source.Close()
	hashBytes, _ := hex.DecodeString(request.SHA256)
	return store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(store.bucket),
		Key:            aws.String(request.Key),
		Body:           source,
		ContentLength:  aws.Int64(request.Size),
		ContentType:    optionalString(request.ContentType),
		Metadata:       map[string]string{sha256MetadataKey: request.SHA256},
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(hashBytes)),
		IfNoneMatch:    aws.String("*"),
	})
}

func (store *S3Store) verifyExisting(ctx context.Context, request PutRequest, created *bool) (Object, error) {
	head, err := store.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(store.bucket),
		Key:          aws.String(request.Key),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Object{}, ctxErr
		}
		return Object{}, errors.New("S3 object could not be verified")
	}
	if head.ContentLength == nil || aws.ToInt64(head.ContentLength) != request.Size || head.Metadata[sha256MetadataKey] != request.SHA256 {
		return Object{}, ErrConflict
	}
	expectedChecksum := sha256Checksum(request.SHA256)
	if head.ChecksumSHA256 != nil {
		if aws.ToString(head.ChecksumSHA256) != expectedChecksum {
			return Object{}, ErrConflict
		}
	} else if err := store.verifyExistingBody(ctx, request, aws.ToString(head.VersionId)); err != nil {
		return Object{}, err
	}
	storedAt := time.Now().UTC()
	if head.LastModified != nil {
		storedAt = head.LastModified.UTC()
	}
	return newObject("s3", request, storedAt, aws.ToString(head.ETag), aws.ToString(head.VersionId), created), nil
}

func (store *S3Store) verifyExistingBody(ctx context.Context, request PutRequest, versionID string) error {
	output, err := store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:    aws.String(store.bucket),
		Key:       aws.String(request.Key),
		VersionId: optionalString(versionID),
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errors.New("S3 object body could not be verified")
	}
	if output.Body == nil {
		return errors.New("S3 object body could not be verified")
	}
	hash := sha256.New()
	reader := contextReader{ctx: ctx, reader: output.Body}
	read, readErr := io.CopyN(hash, reader, request.Size)
	var extra [1]byte
	extraRead, extraErr := reader.Read(extra[:])
	closeErr := output.Body.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errors.New("S3 object body could not be verified")
	}
	if closeErr != nil {
		return errors.New("S3 object body could not be closed after verification")
	}
	if extraErr != nil && !errors.Is(extraErr, io.EOF) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errors.New("S3 object body could not be verified")
	}
	if read != request.Size || extraRead != 0 || hex.EncodeToString(hash.Sum(nil)) != request.SHA256 {
		return ErrConflict
	}
	return nil
}

type s3ErrorClass int

const (
	s3ErrorDefinitive s3ErrorClass = iota
	s3ErrorPrecondition
	s3ErrorConditionalConflict
	s3ErrorAmbiguous
)

func classifyS3PutError(err error) s3ErrorClass {
	var statusError interface{ HTTPStatusCode() int }
	if errors.As(err, &statusError) {
		switch statusError.HTTPStatusCode() {
		case http.StatusPreconditionFailed:
			return s3ErrorPrecondition
		case http.StatusConflict:
			return s3ErrorConditionalConflict
		default:
			return s3ErrorDefinitive
		}
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "PreconditionFailed":
			return s3ErrorPrecondition
		case "ConditionalRequestConflict":
			return s3ErrorConditionalConflict
		default:
			return s3ErrorDefinitive
		}
	}
	var connectionError interface{ ConnectionError() bool }
	if errors.As(err, &connectionError) && connectionError.ConnectionError() {
		return s3ErrorAmbiguous
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return s3ErrorAmbiguous
	}
	return s3ErrorDefinitive
}

func waitForS3Retry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sha256Checksum(value string) string {
	digest, _ := hex.DecodeString(value)
	return base64.StdEncoding.EncodeToString(digest)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return aws.String(value)
}
