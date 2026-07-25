package gtfs

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	metroArchiveName  = "2/google_transit.zip"
	maxMetroZipBytes  = 256 << 20
	maxRouteRecords   = 10_000
	maxStopRecords    = 100_000
	maxTripRecords    = 500_000
	maxStopTimeRows   = 5_000_000
	maxRelations      = 250_000
	maxCSVRecordBytes = 1 << 20
)

var requiredFileLimits = map[string]uint64{
	"routes.txt":     10 << 20,
	"stops.txt":      50 << 20,
	"trips.txt":      100 << 20,
	"stop_times.txt": 1 << 30,
}

func ParseArchive(path, tempDir string) (dataset Dataset, returnErr error) {
	outer, err := zip.OpenReader(path)
	if err != nil {
		return Dataset{}, fmt.Errorf("open GTFS archive: %w", err)
	}
	defer outer.Close()

	metroEntry, err := findUniqueEntry(outer.File, metroArchiveName)
	if err != nil {
		return Dataset{}, err
	}
	if metroEntry.UncompressedSize64 > maxMetroZipBytes {
		return Dataset{}, fmt.Errorf("Metro GTFS archive exceeds %d bytes", maxMetroZipBytes)
	}
	metroPath, err := extractEntry(metroEntry, tempDir)
	if err != nil {
		return Dataset{}, err
	}
	defer func() {
		if err := removeFile(metroPath); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()

	metro, err := zip.OpenReader(metroPath)
	if err != nil {
		return Dataset{}, fmt.Errorf("open nested Metro GTFS archive: %w", err)
	}
	defer metro.Close()
	entries := make(map[string]*zip.File, len(requiredFileLimits))
	for name, limit := range requiredFileLimits {
		entry, err := findUniqueEntry(metro.File, name)
		if err != nil {
			return Dataset{}, err
		}
		if entry.UncompressedSize64 > limit {
			return Dataset{}, fmt.Errorf("%s exceeds %d bytes", name, limit)
		}
		entries[name] = entry
	}

	routes, routeIDs, err := parseRoutes(entries["routes.txt"])
	if err != nil {
		return Dataset{}, err
	}
	stops, stopStations, stations, err := parseStops(entries["stops.txt"])
	if err != nil {
		return Dataset{}, err
	}
	tripRoutes, tripCount, err := parseTrips(entries["trips.txt"], routeIDs)
	if err != nil {
		return Dataset{}, err
	}
	routeStations, stopTimeCount, skippedStopTimes, err := parseStopTimes(
		entries["stop_times.txt"], tripRoutes, stopStations, stations,
	)
	if err != nil {
		return Dataset{}, err
	}
	if len(routes) == 0 || len(stops) == 0 || len(stations) == 0 || tripCount == 0 || stopTimeCount == 0 || len(routeStations) == 0 {
		return Dataset{}, fmt.Errorf(
			"Metro GTFS dataset must be non-empty: routes=%d stops=%d stations=%d trips=%d stop_times=%d route_stations=%d",
			len(routes), len(stops), len(stations), tripCount, stopTimeCount, len(routeStations),
		)
	}

	return Dataset{
		Routes:        routes,
		Stops:         stops,
		RouteStations: routeStations,
		Summary: Summary{
			RouteCount:           len(routes),
			StopCount:            len(stops),
			StationCount:         len(stations),
			TripCount:            tripCount,
			StopTimeCount:        stopTimeCount,
			RouteStationCount:    len(routeStations),
			SkippedStopTimeCount: skippedStopTimes,
			MetroArchiveBytes:    int64(metroEntry.UncompressedSize64),
		},
	}, nil
}

func parseRoutes(file *zip.File) ([]Route, map[string]struct{}, error) {
	rows, closeFile, err := csvRows(file, []string{
		"route_id", "route_type",
	})
	if err != nil {
		return nil, nil, err
	}
	defer closeFile()
	var routes []Route
	ids := make(map[string]struct{})
	for rows.Next() {
		routeType, err := requiredInt(rows.Value("route_type"), "route_type", rows.Line())
		if err != nil {
			return nil, nil, err
		}
		id := rows.Value("route_id")
		if id == "" {
			return nil, nil, fmt.Errorf("routes.txt line %d: route_id is required", rows.Line())
		}
		if _, exists := ids[id]; exists {
			return nil, nil, fmt.Errorf("routes.txt line %d: duplicate route_id %q", rows.Line(), id)
		}
		ids[id] = struct{}{}
		shortName := rows.Value("route_short_name")
		longName := rows.Value("route_long_name")
		if shortName == "" && longName == "" {
			return nil, nil, fmt.Errorf("routes.txt line %d: route_short_name or route_long_name is required", rows.Line())
		}
		routes = append(routes, Route{
			ID:               id,
			AgencyID:         rows.Value("agency_id"),
			ShortName:        shortName,
			LongName:         longName,
			Type:             routeType,
			Color:            strings.ToUpper(rows.Value("route_color")),
			TextColor:        strings.ToUpper(rows.Value("route_text_color")),
			IsReplacementBus: strings.EqualFold(shortName, "Replacement Bus"),
		})
		if len(routes) > maxRouteRecords {
			return nil, nil, fmt.Errorf("routes.txt exceeds %d records", maxRouteRecords)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return routes, ids, nil
}

func parseStops(file *zip.File) ([]Stop, map[string]string, map[string]struct{}, error) {
	rows, closeFile, err := csvRows(file, []string{"stop_id"})
	if err != nil {
		return nil, nil, nil, err
	}
	defer closeFile()
	var stops []Stop
	stations := make(map[string]struct{})
	locationTypes := make(map[string]int)
	parents := make(map[string]string)
	for rows.Next() {
		id := rows.Value("stop_id")
		if id == "" {
			return nil, nil, nil, fmt.Errorf("stops.txt line %d: stop_id is required", rows.Line())
		}
		if _, exists := locationTypes[id]; exists {
			return nil, nil, nil, fmt.Errorf("stops.txt line %d: duplicate stop_id %q", rows.Line(), id)
		}
		latitude, err := optionalFloatPointer(rows.Value("stop_lat"), "stop_lat", rows.Line())
		if err != nil {
			return nil, nil, nil, err
		}
		longitude, err := optionalFloatPointer(rows.Value("stop_lon"), "stop_lon", rows.Line())
		if err != nil {
			return nil, nil, nil, err
		}
		locationType, err := optionalInt(rows.Value("location_type"), 0, "location_type", rows.Line())
		if err != nil {
			return nil, nil, nil, err
		}
		if locationType < 0 || locationType > 4 {
			return nil, nil, nil, fmt.Errorf("stops.txt line %d: unsupported location_type %d", rows.Line(), locationType)
		}
		name := rows.Value("stop_name")
		if locationType <= 2 && name == "" {
			return nil, nil, nil, fmt.Errorf("stops.txt line %d: stop_name is required for location_type %d", rows.Line(), locationType)
		}
		if locationType <= 2 && (latitude == nil || longitude == nil) {
			return nil, nil, nil, fmt.Errorf("stops.txt line %d: stop_lat and stop_lon are required for location_type %d", rows.Line(), locationType)
		}
		wheelchair, err := optionalIntPointer(rows.Value("wheelchair_boarding"), "wheelchair_boarding", rows.Line())
		if err != nil {
			return nil, nil, nil, err
		}
		parent := rows.Value("parent_station")
		locationTypes[id] = locationType
		parents[id] = parent
		if locationType == 1 {
			stations[id] = struct{}{}
		}
		stops = append(stops, Stop{
			ID:                 id,
			Name:               name,
			Latitude:           latitude,
			Longitude:          longitude,
			URL:                rows.Value("stop_url"),
			LocationType:       locationType,
			ParentStationID:    parent,
			WheelchairBoarding: wheelchair,
			LevelID:            rows.Value("level_id"),
			PlatformCode:       rows.Value("platform_code"),
		})
		if len(stops) > maxStopRecords {
			return nil, nil, nil, fmt.Errorf("stops.txt exceeds %d records", maxStopRecords)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	for stopID, locationType := range locationTypes {
		parentID := parents[stopID]
		expectedParentType := -1
		switch locationType {
		case 0:
			if parentID != "" {
				expectedParentType = 1
			}
		case 1:
			if parentID != "" {
				return nil, nil, nil, fmt.Errorf("stops.txt: station %q must not have a parent", stopID)
			}
		case 2, 3:
			expectedParentType = 1
		case 4:
			expectedParentType = 0
		}
		if expectedParentType >= 0 {
			if parentID == "" {
				return nil, nil, nil, fmt.Errorf("stops.txt: stop %q requires a parent for location_type %d", stopID, locationType)
			}
			parentType, exists := locationTypes[parentID]
			if !exists || parentType != expectedParentType {
				return nil, nil, nil, fmt.Errorf(
					"stops.txt: stop %q requires parent location_type %d, got %q",
					stopID, expectedParentType, parentID,
				)
			}
		}
	}
	stopStations := make(map[string]string)
	for stopID, locationType := range locationTypes {
		if locationType != 0 && locationType != 4 {
			continue
		}
		stationID, err := resolveStation(stopID, locationTypes, parents, make(map[string]bool))
		if err != nil {
			return nil, nil, nil, err
		}
		if stationID != "" {
			stopStations[stopID] = stationID
		}
	}
	return stops, stopStations, stations, nil
}

func resolveStation(
	stopID string,
	locationTypes map[string]int,
	parents map[string]string,
	visiting map[string]bool,
) (string, error) {
	if visiting[stopID] {
		return "", fmt.Errorf("stops.txt: parent cycle at stop %q", stopID)
	}
	visiting[stopID] = true
	defer delete(visiting, stopID)
	if locationTypes[stopID] == 1 {
		return stopID, nil
	}
	parentID := parents[stopID]
	if parentID == "" {
		return "", nil
	}
	return resolveStation(parentID, locationTypes, parents, visiting)
}

func parseTrips(file *zip.File, routes map[string]struct{}) (map[string]string, int, error) {
	rows, closeFile, err := csvRows(file, []string{"route_id", "trip_id"})
	if err != nil {
		return nil, 0, err
	}
	defer closeFile()
	trips := make(map[string]string)
	for rows.Next() {
		tripID := rows.Value("trip_id")
		routeID := rows.Value("route_id")
		if tripID == "" || routeID == "" {
			return nil, 0, fmt.Errorf("trips.txt line %d: trip_id and route_id are required", rows.Line())
		}
		if _, exists := routes[routeID]; !exists {
			return nil, 0, fmt.Errorf("trips.txt line %d: unknown route_id %q", rows.Line(), routeID)
		}
		if _, exists := trips[tripID]; exists {
			return nil, 0, fmt.Errorf("trips.txt line %d: duplicate trip_id %q", rows.Line(), tripID)
		}
		trips[tripID] = routeID
		if len(trips) > maxTripRecords {
			return nil, 0, fmt.Errorf("trips.txt exceeds %d records", maxTripRecords)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return trips, len(trips), nil
}

func parseStopTimes(
	file *zip.File,
	trips map[string]string,
	stopStations map[string]string,
	stations map[string]struct{},
) ([]RouteStation, int, int, error) {
	rows, closeFile, err := csvRows(file, []string{"trip_id", "stop_id"})
	if err != nil {
		return nil, 0, 0, err
	}
	defer closeFile()
	relations := make(map[RouteStation]struct{})
	count := 0
	skipped := 0
	for rows.Next() {
		count++
		if count > maxStopTimeRows {
			return nil, 0, 0, fmt.Errorf("stop_times.txt exceeds %d records", maxStopTimeRows)
		}
		tripID := rows.Value("trip_id")
		routeID, tripExists := trips[tripID]
		if !tripExists {
			return nil, 0, 0, fmt.Errorf("stop_times.txt line %d: unknown trip_id %q", rows.Line(), tripID)
		}
		stopID := rows.Value("stop_id")
		stationID, stopExists := stopStations[stopID]
		if !stopExists {
			return nil, 0, 0, fmt.Errorf("stop_times.txt line %d: stop_id %q does not map to a parent station", rows.Line(), stopID)
		}
		if _, stationExists := stations[stationID]; !stationExists {
			return nil, 0, 0, fmt.Errorf("stop_times.txt line %d: unknown station_id %q", rows.Line(), stationID)
		}
		relations[RouteStation{RouteID: routeID, StationID: stationID}] = struct{}{}
		if len(relations) > maxRelations {
			return nil, 0, 0, fmt.Errorf("route-station relationships exceed %d records", maxRelations)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	result := make([]RouteStation, 0, len(relations))
	for relation := range relations {
		result = append(result, relation)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].RouteID == result[j].RouteID {
			return result[i].StationID < result[j].StationID
		}
		return result[i].RouteID < result[j].RouteID
	})
	return result, count, skipped, nil
}

func findUniqueEntry(files []*zip.File, name string) (*zip.File, error) {
	var match *zip.File
	for _, file := range files {
		if file.Name != name {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("GTFS archive contains duplicate %q", name)
		}
		match = file
	}
	if match == nil {
		return nil, fmt.Errorf("GTFS archive is missing %q", name)
	}
	return match, nil
}

func extractEntry(file *zip.File, tempDir string) (path string, returnErr error) {
	reader, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("open %s: %w", file.Name, err)
	}
	defer reader.Close()
	temporary, err := os.CreateTemp(tempDir, "transit-observatory-metro-*.zip")
	if err != nil {
		return "", fmt.Errorf("create temporary Metro GTFS archive: %w", err)
	}
	path = temporary.Name()
	keep := false
	closed := false
	defer func() {
		if !closed {
			returnErr = errors.Join(returnErr, temporary.Close())
		}
		if !keep {
			returnErr = errors.Join(returnErr, removeFile(path))
		}
	}()
	written, err := io.Copy(temporary, io.LimitReader(reader, maxMetroZipBytes+1))
	if err != nil {
		return "", fmt.Errorf("extract Metro GTFS archive: %w", err)
	}
	if written > maxMetroZipBytes {
		return "", fmt.Errorf("Metro GTFS archive exceeds %d bytes", maxMetroZipBytes)
	}
	err = temporary.Close()
	closed = true
	if err != nil {
		return "", fmt.Errorf("close temporary Metro GTFS archive: %w", err)
	}
	keep = true
	return path, nil
}

type rowReader struct {
	fileName string
	reader   *csv.Reader
	columns  map[string]int
	record   []string
	err      error
	line     int
}

func csvRows(file *zip.File, required []string) (*rowReader, func(), error) {
	reader, err := file.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", file.Name, err)
	}
	csvReader := csv.NewReader(&recordLimitReader{reader: reader, remaining: maxCSVRecordBytes})
	csvReader.ReuseRecord = true
	header, err := csvReader.Read()
	if err != nil {
		reader.Close()
		return nil, nil, fmt.Errorf("read %s header: %w", file.Name, err)
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if _, exists := columns[name]; exists {
			reader.Close()
			return nil, nil, fmt.Errorf("%s has duplicate column %q", file.Name, name)
		}
		columns[name] = index
	}
	for _, name := range required {
		if _, exists := columns[name]; !exists {
			reader.Close()
			return nil, nil, fmt.Errorf("%s is missing required column %q", file.Name, name)
		}
	}
	return &rowReader{fileName: file.Name, reader: csvReader, columns: columns, line: 1}, func() { reader.Close() }, nil
}

func (r *rowReader) Next() bool {
	if r.err != nil {
		return false
	}
	record, err := r.reader.Read()
	if err == io.EOF {
		return false
	}
	if err != nil {
		r.err = fmt.Errorf("read %s near record %d: %w", r.fileName, r.line+1, err)
		return false
	}
	r.line++
	r.record = record
	return true
}

func (r *rowReader) Value(column string) string {
	index, exists := r.columns[column]
	if !exists {
		return ""
	}
	if index >= len(r.record) {
		return ""
	}
	return r.record[index]
}

func (r *rowReader) Line() int {
	return r.line
}

func (r *rowReader) Err() error {
	return r.err
}

func requiredInt(value, field string, line int) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("line %d: %s is required", line, field)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("line %d: invalid %s %q: %w", line, field, value, err)
	}
	return parsed, nil
}

func optionalInt(value string, fallback int, field string, line int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	return requiredInt(value, field, line)
}

func optionalIntPointer(value, field string, line int) (*int, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := requiredInt(value, field, line)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func optionalFloatPointer(value, field string, line int) (*float64, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, fmt.Errorf("line %d: invalid %s %q: %w", line, field, value, err)
	}
	return &parsed, nil
}

type recordLimitReader struct {
	reader       io.Reader
	remaining    int
	failed       bool
	inQuotes     bool
	quotePending bool
}

func (r *recordLimitReader) Read(buffer []byte) (int, error) {
	if r.failed {
		return 0, fmt.Errorf("GTFS CSV record exceeds %d bytes", maxCSVRecordBytes)
	}
	count, readErr := r.reader.Read(buffer)
	for index, value := range buffer[:count] {
		r.remaining--
		if r.remaining < 0 {
			r.failed = true
			return index + 1, fmt.Errorf("GTFS CSV record exceeds %d bytes", maxCSVRecordBytes)
		}
		if r.inQuotes {
			if r.quotePending {
				if value == '"' {
					r.quotePending = false
					continue
				}
				r.inQuotes = false
				r.quotePending = false
			} else if value == '"' {
				r.quotePending = true
				continue
			} else {
				continue
			}
		}
		if value == '"' {
			r.inQuotes = true
		} else if value == '\n' {
			r.remaining = maxCSVRecordBytes
		}
	}
	return count, readErr
}
