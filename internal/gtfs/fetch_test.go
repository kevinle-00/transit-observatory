package gtfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDownloaderFetch(t *testing.T) {
	payload := []byte("fixture zip bytes")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.Header.Get("Accept"), "application/zip") {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		response.Header().Set("Content-Type", "application/x-zip-compressed")
		response.Header().Set("ETag", `"fixture"`)
		response.Header().Set("Last-Modified", "Sat, 25 Jul 2026 00:00:00 GMT")
		_, _ = response.Write(payload)
	}))
	defer server.Close()

	download, err := (Downloader{HTTPClient: server.Client(), URL: server.URL, TempDir: t.TempDir()}).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	hash := sha256.Sum256(payload)
	if download.SHA256 != hex.EncodeToString(hash[:]) || download.Size != int64(len(payload)) {
		t.Errorf("download = %#v", download)
	}
	if download.ETag != `"fixture"` || download.LastModified == "" {
		t.Errorf("source metadata = %#v", download)
	}
	if download.RequestedAt.IsZero() || download.ModifiedAt == nil {
		t.Errorf("source timing metadata = %#v", download)
	}
	if _, err := os.Stat(download.Path); err != nil {
		t.Fatalf("temporary archive does not exist: %v", err)
	}
	if err := download.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Stat(download.Path); !os.IsNotExist(err) {
		t.Fatalf("temporary archive still exists: %v", err)
	}
}

func TestDownloaderRejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = response.Write([]byte("not a zip"))
	}))
	defer server.Close()
	_, err := (Downloader{HTTPClient: server.Client(), URL: server.URL, TempDir: t.TempDir()}).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected GTFS content type") {
		t.Fatalf("Fetch() error = %v", err)
	}
}
