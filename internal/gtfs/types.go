package gtfs

import "time"

type Route struct {
	ID               string
	AgencyID         string
	ShortName        string
	LongName         string
	Type             int
	Color            string
	TextColor        string
	IsReplacementBus bool
}

type Stop struct {
	ID                 string
	Name               string
	Latitude           *float64
	Longitude          *float64
	URL                string
	LocationType       int
	ParentStationID    string
	WheelchairBoarding *int
	LevelID            string
	PlatformCode       string
}

type RouteStation struct {
	RouteID   string
	StationID string
}

type Dataset struct {
	Routes        []Route
	Stops         []Stop
	RouteStations []RouteStation
	Summary       Summary
}

type Summary struct {
	RouteCount           int   `json:"route_count"`
	StopCount            int   `json:"stop_count"`
	StationCount         int   `json:"station_count"`
	TripCount            int   `json:"trip_count"`
	StopTimeCount        int   `json:"stop_time_count"`
	RouteStationCount    int   `json:"route_station_count"`
	SkippedStopTimeCount int   `json:"skipped_stop_time_count"`
	MetroArchiveBytes    int64 `json:"metro_archive_bytes"`
}

type Download struct {
	Path         string
	SourceURL    string
	RequestedAt  time.Time
	RetrievedAt  time.Time
	ContentType  string
	ETag         string
	LastModified string
	ModifiedAt   *time.Time
	SHA256       string
	Size         int64
}

func (d Download) Cleanup() error {
	return removeFile(d.Path)
}
