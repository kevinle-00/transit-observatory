package storage

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCanonicalKeys(t *testing.T) {
	retrievedAt := time.Date(2026, 7, 26, 14, 3, 2, 123, time.FixedZone("offset", 10*60*60))
	alerts, err := ServiceAlertsKey(retrievedAt, testHash)
	if err != nil {
		t.Fatal(err)
	}
	if want := "raw/service-alerts/2026/07/26/04/20260726T040302.000000123Z-" + testHash + ".pb"; alerts != want {
		t.Errorf("ServiceAlertsKey() = %q, want %q", alerts, want)
	}
	gtfs, err := GTFSKey(retrievedAt, testHash)
	if err != nil {
		t.Fatal(err)
	}
	if want := "raw/gtfs/2026/07/26/20260726T040302.000000123Z-" + testHash + ".zip"; gtfs != want {
		t.Errorf("GTFSKey() = %q, want %q", gtfs, want)
	}
}

func TestCanonicalKeysRejectInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		time time.Time
		hash string
	}{{hash: testHash}, {time: time.Now(), hash: strings.ToUpper(testHash)}, {time: time.Now(), hash: "short"}} {
		if _, err := ServiceAlertsKey(test.time, test.hash); err == nil {
			t.Errorf("ServiceAlertsKey(%v, %q) error = nil", test.time, test.hash)
		}
		if _, err := GTFSKey(test.time, test.hash); err == nil {
			t.Errorf("GTFSKey(%v, %q) error = nil", test.time, test.hash)
		}
	}
}

func TestValidateKeyRejectsUnsafeKeys(t *testing.T) {
	invalid := []string{"", "/raw/file", "../raw", "raw/../file", "raw//file", "raw/./file", `raw\file`, "raw/\x00file", "raw/file/"}
	for _, key := range invalid {
		if err := ValidateKey(key); err == nil {
			t.Errorf("ValidateKey(%q) error = nil", key)
		}
	}
	if err := ValidateKey("raw/gtfs/2026/file.zip"); err != nil {
		t.Errorf("ValidateKey(valid) = %v", err)
	}
}

func TestByteAndFileSourcesReopen(t *testing.T) {
	data := []byte("exact bytes")
	bytesSource := BytesSource(data)
	data[0] = 'X'
	assertSourceContents(t, bytesSource, "exact bytes")
	assertSourceContents(t, bytesSource, "exact bytes")

	path := filepath.Join(t.TempDir(), "source.zip")
	if err := os.WriteFile(path, []byte("file bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileSource, err := FileSource(path)
	if err != nil {
		t.Fatal(err)
	}
	assertSourceContents(t, fileSource, "file bytes")
	assertSourceContents(t, fileSource, "file bytes")
}

func assertSourceContents(t *testing.T, source Source, want string) {
	t.Helper()
	reader, err := source.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want || source.Size() != int64(len(want)) {
		t.Fatalf("source = %q size %d, want %q size %d", got, source.Size(), want, len(want))
	}
}
