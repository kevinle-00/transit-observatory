package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLocalStorePutAndIdempotency(t *testing.T) {
	root := t.TempDir()
	store, _ := NewLocalStore(root)
	request := requestFor("raw/service-alerts/2026/object.pb", []byte("payload"))
	created, err := store.Put(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !createdIs(created, true) || created.Backend != "local" || created.Key != request.Key || created.SHA256 != request.SHA256 {
		t.Fatalf("created object = %+v", created)
	}
	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(request.Key)))
	if err != nil || string(stored) != "payload" {
		t.Fatalf("stored content = %q, error = %v", stored, err)
	}
	existing, err := store.Put(context.Background(), request)
	if err != nil || !createdIs(existing, false) {
		t.Fatalf("idempotent Put() = %+v, %v", existing, err)
	}
	assertNoTemporaryFiles(t, root)
}

func TestLocalStoreRejectsIntegrityAndTraversal(t *testing.T) {
	store, _ := NewLocalStore(t.TempDir())
	valid := requestFor("raw/object", []byte("payload"))
	tests := []PutRequest{
		{Key: "../outside", Source: valid.Source, Size: valid.Size, SHA256: valid.SHA256},
		{Key: valid.Key, Source: valid.Source, Size: valid.Size + 1, SHA256: valid.SHA256},
		{Key: valid.Key, Source: valid.Source, Size: valid.Size, SHA256: stringsOf('a', 64)},
	}
	for _, request := range tests {
		if _, err := store.Put(context.Background(), request); err == nil {
			t.Errorf("Put(%+v) error = nil", request)
		}
	}
}

func TestLocalStoreExistingConflict(t *testing.T) {
	root := t.TempDir()
	store, _ := NewLocalStore(root)
	request := requestFor("raw/object", []byte("payload"))
	path := filepath.Join(root, "raw", "object")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("payloae"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put() error = %v, want ErrConflict", err)
	}
}

func TestLocalStoreCleansUpOnReadFailureAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name   string
		ctx    context.Context
		source Source
	}{
		{name: "read failure", ctx: context.Background(), source: failingSource{size: 7}},
		{name: "cancellation", ctx: canceledContext(), source: BytesSource([]byte("payload"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, _ := NewLocalStore(root)
			request := requestFor("raw/object", []byte("payload"))
			request.Source = test.source
			if _, err := store.Put(test.ctx, request); err == nil {
				t.Fatal("Put() error = nil")
			} else if strings.Contains(err.Error(), "do-not-expose") || strings.Contains(err.Error(), root) {
				t.Fatalf("Put() exposed private error detail: %v", err)
			}
			assertNoTemporaryFiles(t, root)
		})
	}
}

func TestLocalStoreRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "raw")); err != nil {
		t.Fatal(err)
	}
	store, _ := NewLocalStore(root)
	request := requestFor("raw/object", []byte("payload"))
	if _, err := store.Put(context.Background(), request); err == nil {
		t.Fatal("Put() through escaping symlink error = nil")
	}
	if _, err := os.Stat(filepath.Join(outside, "object")); !os.IsNotExist(err) {
		t.Fatalf("outside object exists or stat failed unexpectedly: %v", err)
	}
}

func TestLocalStoreConcurrentCreate(t *testing.T) {
	root := t.TempDir()
	store, _ := NewLocalStore(root)
	request := requestFor("raw/gtfs/2026/object.zip", bytes.Repeat([]byte("x"), 128*1024))
	const writers = 12
	objects := make(chan Object, writers)
	errorsChannel := make(chan error, writers)
	var wait sync.WaitGroup
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			object, err := store.Put(context.Background(), request)
			objects <- object
			errorsChannel <- err
		}()
	}
	wait.Wait()
	close(objects)
	close(errorsChannel)
	created := 0
	for err := range errorsChannel {
		if err != nil {
			t.Errorf("concurrent Put() error = %v", err)
		}
	}
	for object := range objects {
		if createdIs(object, true) {
			created++
		}
	}
	if created != 1 {
		t.Errorf("created count = %d, want 1", created)
	}
	assertNoTemporaryFiles(t, root)
}

func createdIs(object Object, want bool) bool {
	return object.Created != nil && *object.Created == want
}

func requestFor(key string, data []byte) PutRequest {
	hash := sha256.Sum256(data)
	return PutRequest{Key: key, Source: BytesSource(data), Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), ContentType: "application/octet-stream"}
}

func assertNoTemporaryFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry != nil && !entry.IsDir() && len(entry.Name()) >= len(".raw-storage-") && entry.Name()[:len(".raw-storage-")] == ".raw-storage-" {
			t.Errorf("temporary file remains: %s", entry.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

type failingSource struct{ size int64 }

func (source failingSource) Size() int64 { return source.size }
func (source failingSource) Open() (io.ReadSeekCloser, error) {
	return &failingReader{Reader: bytes.NewReader([]byte("payload"))}, nil
}

type failingReader struct{ *bytes.Reader }

func (reader *failingReader) Read([]byte) (int, error) {
	return 0, errors.New("credential=do-not-expose")
}
func (*failingReader) Close() error { return nil }

func stringsOf(character byte, count int) string {
	return string(bytes.Repeat([]byte{character}, count))
}
