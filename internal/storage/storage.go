package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var (
	ErrConflict = errors.New("stored object conflicts with requested content")
	hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Store interface {
	Put(context.Context, PutRequest) (Object, error)
}

// Source can be opened repeatedly and reports its exact byte size.
type Source interface {
	Open() (io.ReadSeekCloser, error)
	Size() int64
}

type PutRequest struct {
	Key         string
	Source      Source
	Size        int64
	SHA256      string
	ContentType string
}

type Object struct {
	Backend   string
	Key       string
	Size      int64
	SHA256    string
	StoredAt  time.Time
	ETag      string
	VersionID string
	Created   *bool
}

func Created(value bool) *bool { return &value }

func validateRequest(request PutRequest) error {
	if err := ValidateKey(request.Key); err != nil {
		return err
	}
	if request.Source == nil {
		return errors.New("storage source is required")
	}
	if request.Size < 0 {
		return errors.New("storage size must not be negative")
	}
	if request.Source.Size() != request.Size {
		return fmt.Errorf("storage source size does not match expected size")
	}
	if !hashPattern.MatchString(request.SHA256) {
		return errors.New("storage SHA256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func ValidateKey(key string) error {
	if key == "" {
		return errors.New("storage key is required")
	}
	if strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return errors.New("storage key must be a relative slash-separated path")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("storage key contains an invalid path segment")
		}
		for _, character := range segment {
			if character < 0x20 || character == 0x7f {
				return errors.New("storage key contains a control character")
			}
		}
	}
	return nil
}
