package database

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

const (
	AlertStatusPresent    = "present"
	AlertStatusCurrent    = "current"
	AlertStatusUpcoming   = "upcoming"
	AlertStatusHistorical = "historical"
)

type AlertQuery struct {
	Status    string
	Now       time.Time
	LineID    string
	StationID string
	Cause     string
	Effect    string
	From      *time.Time
	To        *time.Time
	Page      int
	PageSize  int
}

type AlertPage struct {
	Alerts   []CurrentAlert `json:"alerts"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Total    int            `json:"total"`
}

type AlertDetail struct {
	ID             int64         `json:"id"`
	SourceURL      string        `json:"source_url"`
	SourceEntityID string        `json:"source_entity_id"`
	Status         string        `json:"status"`
	FirstSeenAt    time.Time     `json:"first_seen_at"`
	LastSeenAt     time.Time     `json:"last_seen_at"`
	ClosedAt       *time.Time    `json:"closed_at,omitempty"`
	RevisionCount  int           `json:"revision_count"`
	LatestRevision AlertRevision `json:"latest_revision"`
}

type AlertRevision struct {
	CurrentAlert
	IsDeleted bool       `json:"is_deleted"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

type LineSummary struct {
	ID                 string  `json:"id"`
	ShortName          string  `json:"short_name"`
	LongName           string  `json:"long_name"`
	RouteType          int     `json:"route_type"`
	Color              *string `json:"color,omitempty"`
	TextColor          *string `json:"text_color,omitempty"`
	IsReplacementBus   bool    `json:"is_replacement_bus"`
	StationCount       int     `json:"station_count"`
	PresentAlertCount  int     `json:"present_alert_count"`
	CurrentAlertCount  int     `json:"current_alert_count"`
	UpcomingAlertCount int     `json:"upcoming_alert_count"`
}

type LineDetail struct {
	Line     LineSummary      `json:"line"`
	Stations []StationSummary `json:"stations"`
	Alerts   []CurrentAlert   `json:"alerts"`
}

type StationQuery struct {
	Q      string
	LineID string
}

type StationSummary struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Latitude           *float64      `json:"latitude,omitempty"`
	Longitude          *float64      `json:"longitude,omitempty"`
	WheelchairBoarding *int          `json:"wheelchair_boarding,omitempty"`
	Lines              []LineSummary `json:"lines"`
	PresentAlertCount  int           `json:"present_alert_count"`
	CurrentAlertCount  int           `json:"current_alert_count"`
	UpcomingAlertCount int           `json:"upcoming_alert_count"`
}

type StationDetail struct {
	Station StationSummary `json:"station"`
	Alerts  []CurrentAlert `json:"alerts"`
}

type AnalyticsQuery struct {
	Now                   time.Time
	From                  time.Time
	To                    time.Time
	Interval              string
	IncludeReplacementBus bool
}

type AnalyticsPoint struct {
	StartsAt                      time.Time `json:"starts_at"`
	AlertCount                    int       `json:"alert_count"`
	CompletedEpisodeSampleCount   int       `json:"completed_episode_sample_count"`
	MedianObservedLifetimeSeconds *float64  `json:"median_observed_lifetime_seconds,omitempty"`
}

type AnalyticsBreakdown struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type LineAnalytics struct {
	Line              LineSummary          `json:"line"`
	Series            []AnalyticsPoint     `json:"series"`
	Causes            []AnalyticsBreakdown `json:"causes"`
	Effects           []AnalyticsBreakdown `json:"effects"`
	MetricLimitations []string             `json:"metric_limitations"`
}
