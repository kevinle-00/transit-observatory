package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type s3PutResult struct {
	output *s3.PutObjectOutput
	err    error
}

type mockS3Client struct {
	putInputs  []*s3.PutObjectInput
	putResults []s3PutResult
	headInput  *s3.HeadObjectInput
	headOutput *s3.HeadObjectOutput
	headErr    error
	headCalls  int
	getInput   *s3.GetObjectInput
	getOutput  *s3.GetObjectOutput
	getErr     error
	getCalls   int
}

func (client *mockS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	client.putInputs = append(client.putInputs, input)
	if len(client.putResults) == 0 {
		return &s3.PutObjectOutput{}, nil
	}
	result := client.putResults[0]
	client.putResults = client.putResults[1:]
	return result.output, result.err
}

func (client *mockS3Client) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	client.headCalls++
	client.headInput = input
	return client.headOutput, client.headErr
}

func (client *mockS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	client.getCalls++
	client.getInput = input
	return client.getOutput, client.getErr
}

func TestS3StoreCreateIncludesIntegrityMetadata(t *testing.T) {
	client := &mockS3Client{putResults: []s3PutResult{{output: &s3.PutObjectOutput{ETag: aws.String("opaque-etag"), VersionId: aws.String("version")}}}}
	request := requestFor("raw/object", []byte("payload"))
	object, err := newS3Store(client, "private-bucket").Put(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !createdIs(object, true) || object.Backend != "s3" || object.ETag != "opaque-etag" || object.VersionID != "version" {
		t.Fatalf("Put() object = %+v", object)
	}
	input := client.putInputs[0]
	body, err := io.ReadAll(input.Body)
	if err != nil || string(body) != "payload" {
		t.Fatalf("PutObject body = %q, error = %v", body, err)
	}
	if aws.ToString(input.IfNoneMatch) != "*" || input.Metadata[sha256MetadataKey] != request.SHA256 ||
		aws.ToString(input.ChecksumSHA256) != sha256Checksum(request.SHA256) || aws.ToString(input.ContentType) != request.ContentType {
		t.Fatalf("PutObject input = %+v", input)
	}
}

func TestS3StorePreconditionVerifiesExistingWithChecksum(t *testing.T) {
	request := requestFor("raw/object", []byte("payload"))
	storedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	client := &mockS3Client{
		putResults: []s3PutResult{{err: apiError("PreconditionFailed")}},
		headOutput: &s3.HeadObjectOutput{ContentLength: aws.Int64(request.Size), Metadata: map[string]string{sha256MetadataKey: request.SHA256},
			ChecksumSHA256: aws.String(sha256Checksum(request.SHA256)), ETag: aws.String("not-a-hash"), VersionId: aws.String("v2"), LastModified: &storedAt},
	}
	object, err := newS3Store(client, "private-bucket").Put(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !createdIs(object, false) || object.ETag != "not-a-hash" || !object.StoredAt.Equal(storedAt) || client.getCalls != 0 {
		t.Fatalf("Put() object = %+v, get calls = %d", object, client.getCalls)
	}
	if client.headInput.ChecksumMode != types.ChecksumModeEnabled {
		t.Errorf("HeadObject ChecksumMode = %q", client.headInput.ChecksumMode)
	}
}

func TestS3StoreConditionalConflictRetriesPut(t *testing.T) {
	request := requestFor("raw/object", []byte("payload"))
	client := &mockS3Client{putResults: []s3PutResult{
		{err: apiError("ConditionalRequestConflict")},
		{output: &s3.PutObjectOutput{}},
	}}
	object, err := newS3Store(client, "bucket").Put(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.putInputs) != 2 || !createdIs(object, true) {
		t.Fatalf("put calls = %d, object = %+v", len(client.putInputs), object)
	}
}

func TestClassifyS3PutHTTPStatus(t *testing.T) {
	if got := classifyS3PutError(httpStatusError(http.StatusPreconditionFailed)); got != s3ErrorPrecondition {
		t.Errorf("412 classification = %v", got)
	}
	if got := classifyS3PutError(httpStatusError(http.StatusConflict)); got != s3ErrorConditionalConflict {
		t.Errorf("409 classification = %v", got)
	}
	if got := classifyS3PutError(httpStatusError(http.StatusForbidden)); got != s3ErrorDefinitive {
		t.Errorf("403 classification = %v", got)
	}
}

func TestS3StoreDefinitiveFailureDoesNotReconcile(t *testing.T) {
	request := requestFor("raw/object", []byte("payload"))
	secret := "secret-endpoint-or-credential"
	client := &mockS3Client{
		putResults: []s3PutResult{{err: &smithy.GenericAPIError{Code: "AccessDenied", Message: secret, Fault: smithy.FaultClient}}},
		headOutput: matchingHead(request, true),
	}
	_, err := newS3Store(client, "private-bucket").Put(context.Background(), request)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("Put() error = %v", err)
	}
	if client.headCalls != 0 {
		t.Fatalf("HeadObject called %d times after definitive failure", client.headCalls)
	}
}

func TestS3StoreAmbiguousTransportReconcilesWithUnknownCreated(t *testing.T) {
	request := requestFor("raw/object", []byte("payload"))
	client := &mockS3Client{
		putResults: []s3PutResult{{err: &smithyhttp.RequestSendError{Err: errors.New("connection reset at private endpoint")}}},
		headOutput: matchingHead(request, true),
	}
	object, err := newS3Store(client, "private-bucket").Put(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if object.Created != nil {
		t.Fatalf("Created = %v, want nil", object.Created)
	}
}

func TestS3StoreGetsAndHashesWhenHeadChecksumAbsent(t *testing.T) {
	request := requestFor("raw/object", []byte("payload"))
	body := &trackingReadCloser{Reader: strings.NewReader("payload")}
	client := &mockS3Client{
		putResults: []s3PutResult{{err: apiError("PreconditionFailed")}},
		headOutput: matchingHead(request, false),
		getOutput:  &s3.GetObjectOutput{Body: body},
	}
	object, err := newS3Store(client, "bucket").Put(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !createdIs(object, false) || !body.closed || client.getCalls != 1 || aws.ToString(client.getInput.VersionId) != "version" {
		t.Fatalf("object = %+v, body closed = %t, get calls = %d, get input = %+v", object, body.closed, client.getCalls, client.getInput)
	}
}

func TestS3StoreRejectsSpoofedMetadataAndClosesBody(t *testing.T) {
	request := requestFor("raw/object", []byte("payload"))
	for _, bodyValue := range []string{"payloae", "payload-extra"} {
		body := &trackingReadCloser{Reader: strings.NewReader(bodyValue)}
		client := &mockS3Client{
			putResults: []s3PutResult{{err: apiError("PreconditionFailed")}},
			headOutput: matchingHead(request, false),
			getOutput:  &s3.GetObjectOutput{Body: body},
		}
		if _, err := newS3Store(client, "bucket").Put(context.Background(), request); !errors.Is(err, ErrConflict) {
			t.Fatalf("body %q: Put() error = %v, want ErrConflict", bodyValue, err)
		}
		if !body.closed {
			t.Fatalf("body %q was not closed", bodyValue)
		}
	}
}

func TestS3StoreRejectsWrongHeadChecksumWithoutGet(t *testing.T) {
	request := requestFor("raw/object", []byte("payload"))
	head := matchingHead(request, true)
	head.ChecksumSHA256 = aws.String(base64.StdEncoding.EncodeToString(make([]byte, sha256.Size)))
	client := &mockS3Client{putResults: []s3PutResult{{err: apiError("PreconditionFailed")}}, headOutput: head}
	if _, err := newS3Store(client, "bucket").Put(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put() error = %v, want ErrConflict", err)
	}
	if client.getCalls != 0 {
		t.Fatalf("GetObject called %d times", client.getCalls)
	}
}

func matchingHead(request PutRequest, checksum bool) *s3.HeadObjectOutput {
	output := &s3.HeadObjectOutput{
		ContentLength: aws.Int64(request.Size),
		Metadata:      map[string]string{sha256MetadataKey: request.SHA256},
		VersionId:     aws.String("version"),
	}
	if checksum {
		output.ChecksumSHA256 = aws.String(sha256Checksum(request.SHA256))
	}
	return output
}

func apiError(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: "safe", Fault: smithy.FaultClient}
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

type httpStatusError int

func (err httpStatusError) Error() string       { return "HTTP failure" }
func (err httpStatusError) HTTPStatusCode() int { return int(err) }

func (reader *trackingReadCloser) Close() error {
	reader.closed = true
	return nil
}
