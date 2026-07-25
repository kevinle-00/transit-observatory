package gtfs

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseArchive(t *testing.T) {
	path := writeNestedArchive(t, validFixtureFiles())
	dataset, err := ParseArchive(path, t.TempDir())
	if err != nil {
		t.Fatalf("ParseArchive() error = %v", err)
	}
	if dataset.Summary.RouteCount != 2 || dataset.Summary.StopCount != 3 || dataset.Summary.StationCount != 1 {
		t.Errorf("network counts = %#v", dataset.Summary)
	}
	if dataset.Summary.TripCount != 2 || dataset.Summary.StopTimeCount != 2 {
		t.Errorf("schedule counts = %#v", dataset.Summary)
	}
	if dataset.Summary.RouteStationCount != 2 || dataset.Summary.SkippedStopTimeCount != 0 {
		t.Errorf("relation counts = %#v", dataset.Summary)
	}
	if !dataset.Routes[1].IsReplacementBus {
		t.Error("replacement-bus route was not identified")
	}
	if got := dataset.RouteStations; len(got) != 2 || got[0].StationID != "station-a" {
		t.Errorf("RouteStations = %#v", got)
	}
}

func TestParseArchiveRequiresMetroFiles(t *testing.T) {
	files := validFixtureFiles()
	delete(files, "stop_times.txt")
	_, err := ParseArchive(writeNestedArchive(t, files), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `missing "stop_times.txt"`) {
		t.Fatalf("ParseArchive() error = %v, want missing file", err)
	}
}

func TestParseArchiveValidatesColumns(t *testing.T) {
	files := validFixtureFiles()
	files["routes.txt"] = "route_id,route_short_name\nr1,Line One\n"
	_, err := ParseArchive(writeNestedArchive(t, files), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `missing required column "route_type"`) {
		t.Fatalf("ParseArchive() error = %v, want missing column", err)
	}
}

func TestParseArchiveAllowsOptionalColumnsToBeAbsent(t *testing.T) {
	files := validFixtureFiles()
	files["routes.txt"] = "route_id,route_short_name,route_type\nr1,Line One,400\n"
	files["stops.txt"] = "stop_id,stop_name,stop_lat,stop_lon,location_type,parent_station\n" +
		"station-a,Station A,-37.8,144.9,1,\nplatform-a,Station A,-37.8,144.9,0,station-a\n" +
		"node-a,,,,3,station-a\n"
	files["trips.txt"] = "route_id,trip_id\nr1,trip-1\n"
	files["stop_times.txt"] = "trip_id,stop_id\ntrip-1,platform-a\n"
	if _, err := ParseArchive(writeNestedArchive(t, files), t.TempDir()); err != nil {
		t.Fatalf("ParseArchive() optional columns error = %v", err)
	}
}

func TestParseArchiveResolvesBoardingAreaThroughPlatform(t *testing.T) {
	files := validFixtureFiles()
	files["stops.txt"] += "boarding-a,,,,,4,platform-a,0,,\n"
	files["stop_times.txt"] = "trip_id,stop_id\ntrip-1,boarding-a\ntrip-2,platform-a\n"
	dataset, err := ParseArchive(writeNestedArchive(t, files), t.TempDir())
	if err != nil {
		t.Fatalf("ParseArchive() boarding area error = %v", err)
	}
	if dataset.Summary.RouteStationCount != 2 {
		t.Errorf("route-station count = %d, want 2", dataset.Summary.RouteStationCount)
	}
}

func TestParseArchiveRejectsEmptyNetwork(t *testing.T) {
	files := map[string]string{
		"routes.txt":     "route_id,route_type\n",
		"stops.txt":      "stop_id,stop_name,stop_lat,stop_lon\n",
		"trips.txt":      "route_id,trip_id\n",
		"stop_times.txt": "trip_id,stop_id\n",
	}
	_, err := ParseArchive(writeNestedArchive(t, files), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "must be non-empty") {
		t.Fatalf("ParseArchive() error = %v, want empty-network rejection", err)
	}
}

func TestParseArchiveLimitsCSVRecordSize(t *testing.T) {
	files := validFixtureFiles()
	files["routes.txt"] += "oversized,,\"" + strings.Repeat("x", maxCSVRecordBytes/2) + "\n" +
		strings.Repeat("x", maxCSVRecordBytes/2) + "\",Long,400,,\n"
	_, err := ParseArchive(writeNestedArchive(t, files), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "CSV record exceeds") {
		t.Fatalf("ParseArchive() error = %v, want record-size rejection", err)
	}
}

func TestParseArchiveRejectsStopTimeWithoutStationMapping(t *testing.T) {
	files := validFixtureFiles()
	files["stop_times.txt"] += "trip-1,orphan\n"
	_, err := ParseArchive(writeNestedArchive(t, files), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `does not map to a parent station`) {
		t.Fatalf("ParseArchive() error = %v, want station mapping error", err)
	}
}

func TestParseArchiveRejectsStationInStopTimes(t *testing.T) {
	files := validFixtureFiles()
	files["stop_times.txt"] = "trip_id,stop_id\ntrip-1,station-a\n"
	_, err := ParseArchive(writeNestedArchive(t, files), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `does not map to a parent station`) {
		t.Fatalf("ParseArchive() error = %v, want invalid stop-time location rejection", err)
	}
}

func validFixtureFiles() map[string]string {
	return map[string]string{
		"routes.txt": "route_id,agency_id,route_short_name,route_long_name,route_type,route_color,route_text_color\n" +
			"r1,,Line One,Line One - City,400,112233,FFFFFF\n" +
			"r1-R,,Replacement Bus,Line One - City,400,FE5000,FFFFFF\n",
		"stops.txt": "stop_id,stop_name,stop_lat,stop_lon,stop_url,location_type,parent_station,wheelchair_boarding,level_id,platform_code\n" +
			"station-a,Station A,-37.8,144.9,https://example.test/station-a,1,,1,,\n" +
			"platform-a,Station A,-37.8,144.9,,0,station-a,1,level-1,1\n" +
			"orphan,Orphan,-37.9,145.0,,0,,0,,\n",
		"trips.txt":      "route_id,trip_id\nr1,trip-1\nr1-R,trip-2\n",
		"stop_times.txt": "trip_id,stop_id\ntrip-1,platform-a\ntrip-2,platform-a\n",
	}
}

func writeNestedArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	var inner bytes.Buffer
	innerWriter := zip.NewWriter(&inner)
	for name, content := range files {
		entry, err := innerWriter.Create(name)
		if err != nil {
			t.Fatalf("create inner entry: %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write inner entry: %v", err)
		}
	}
	if err := innerWriter.Close(); err != nil {
		t.Fatalf("close inner archive: %v", err)
	}

	path := t.TempDir() + "/gtfs.zip"
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create outer archive: %v", err)
	}
	outerWriter := zip.NewWriter(file)
	entry, err := outerWriter.Create(metroArchiveName)
	if err != nil {
		t.Fatalf("create Metro archive entry: %v", err)
	}
	if _, err := entry.Write(inner.Bytes()); err != nil {
		t.Fatalf("write Metro archive entry: %v", err)
	}
	if err := outerWriter.Close(); err != nil {
		t.Fatalf("close outer archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive file: %v", err)
	}
	return path
}
