package gtfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"
)

const maxArchiveBytes int64 = 1 << 30

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Downloader struct {
	HTTPClient HTTPClient
	URL        string
	TempDir    string
}

func (d Downloader) Fetch(ctx context.Context) (download Download, returnErr error) {
	if d.HTTPClient == nil {
		return Download{}, fmt.Errorf("HTTP client is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return Download{}, fmt.Errorf("create GTFS request: %w", err)
	}
	request.Header.Set("Accept", "application/zip, application/x-zip-compressed, application/octet-stream")
	requestedAt := time.Now().UTC()
	response, err := d.HTTPClient.Do(request)
	if err != nil {
		return Download{}, fmt.Errorf("fetch GTFS archive: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return Download{}, fmt.Errorf("fetch GTFS archive: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if response.ContentLength > maxArchiveBytes {
		return Download{}, fmt.Errorf("GTFS archive content length %d exceeds %d bytes", response.ContentLength, maxArchiveBytes)
	}
	if err := validateZipContentType(response.Header.Get("Content-Type")); err != nil {
		return Download{}, err
	}

	file, err := os.CreateTemp(d.TempDir, "transit-observatory-gtfs-*.zip")
	if err != nil {
		return Download{}, fmt.Errorf("create temporary GTFS archive: %w", err)
	}
	path := file.Name()
	keep := false
	closed := false
	defer func() {
		if !closed {
			returnErr = errors.Join(returnErr, file.Close())
		}
		if !keep {
			returnErr = errors.Join(returnErr, removeFile(path))
		}
	}()

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxArchiveBytes+1))
	if err != nil {
		return Download{}, fmt.Errorf("download GTFS archive: %w", err)
	}
	if written > maxArchiveBytes {
		return Download{}, fmt.Errorf("GTFS archive exceeds %d bytes", maxArchiveBytes)
	}
	if written == 0 {
		return Download{}, fmt.Errorf("GTFS archive is empty")
	}
	err = file.Close()
	closed = true
	if err != nil {
		return Download{}, fmt.Errorf("close temporary GTFS archive: %w", err)
	}
	var modifiedAt *time.Time
	if value := response.Header.Get("Last-Modified"); value != "" {
		parsed, err := http.ParseTime(value)
		if err != nil {
			return Download{}, fmt.Errorf("parse GTFS Last-Modified header: %w", err)
		}
		parsed = parsed.UTC()
		modifiedAt = &parsed
	}
	keep = true
	return Download{
		Path:         path,
		SourceURL:    d.URL,
		RequestedAt:  requestedAt,
		RetrievedAt:  time.Now().UTC(),
		ContentType:  response.Header.Get("Content-Type"),
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
		ModifiedAt:   modifiedAt,
		SHA256:       hex.EncodeToString(hash.Sum(nil)),
		Size:         written,
	}, nil
}

func validateZipContentType(value string) error {
	if value == "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("invalid GTFS content type %q: %w", value, err)
	}
	switch mediaType {
	case "application/zip", "application/x-zip-compressed", "application/octet-stream":
		return nil
	default:
		return fmt.Errorf("unexpected GTFS content type %q", value)
	}
}

func removeFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove temporary GTFS archive: %w", err)
	}
	return nil
}
