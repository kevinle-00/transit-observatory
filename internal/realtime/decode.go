package realtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type FeedSummary struct {
	GTFSRealtimeVersion string         `json:"gtfs_realtime_version"`
	Incrementality      string         `json:"incrementality"`
	Timestamp           *Timestamp     `json:"timestamp,omitempty"`
	EntityCount         int            `json:"entity_count"`
	AlertCount          int            `json:"alert_count"`
	NonAlertEntityCount int            `json:"non_alert_entity_count"`
	UnknownFieldsBytes  int            `json:"unknown_fields_bytes"`
	Alerts              []AlertSummary `json:"alerts"`
}

type AlertSummary struct {
	EntityID           string           `json:"entity_id"`
	Deleted            bool             `json:"deleted"`
	Cause              *string          `json:"cause,omitempty"`
	Effect             *string          `json:"effect,omitempty"`
	Severity           *string          `json:"severity,omitempty"`
	Header             []Translation    `json:"header,omitempty"`
	Description        []Translation    `json:"description,omitempty"`
	URL                []Translation    `json:"url,omitempty"`
	ActivePeriods      []ActivePeriod   `json:"active_periods,omitempty"`
	InformedEntities   []InformedEntity `json:"informed_entities,omitempty"`
	UnknownFieldsBytes int              `json:"unknown_fields_bytes"`
	UnknownFieldsHash  string           `json:"unknown_fields_sha256,omitempty"`
}

type Translation struct {
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
}

type Timestamp struct {
	Unix uint64 `json:"unix"`
	UTC  string `json:"utc,omitempty"`
}

type ActivePeriod struct {
	Start *Timestamp `json:"start,omitempty"`
	End   *Timestamp `json:"end,omitempty"`
}

type InformedEntity struct {
	AgencyID      string  `json:"agency_id,omitempty"`
	RouteID       string  `json:"route_id,omitempty"`
	RouteType     *int32  `json:"route_type,omitempty"`
	StopID        string  `json:"stop_id,omitempty"`
	DirectionID   *uint32 `json:"direction_id,omitempty"`
	TripID        string  `json:"trip_id,omitempty"`
	TripRouteID   string  `json:"trip_route_id,omitempty"`
	TripStartTime string  `json:"trip_start_time,omitempty"`
	TripStartDate string  `json:"trip_start_date,omitempty"`
	TripDirection *uint32 `json:"trip_direction_id,omitempty"`
	TripSchedule  string  `json:"trip_schedule_relationship,omitempty"`
}

func DecodeAlerts(payload []byte) (FeedSummary, error) {
	feed := new(gtfs.FeedMessage)
	if err := proto.Unmarshal(payload, feed); err != nil {
		return FeedSummary{}, fmt.Errorf("decode GTFS-Realtime feed: %w", err)
	}
	if feed.Header == nil {
		return FeedSummary{}, fmt.Errorf("decode GTFS-Realtime feed: header is missing")
	}

	summary := FeedSummary{
		GTFSRealtimeVersion: feed.Header.GetGtfsRealtimeVersion(),
		Incrementality:      feed.Header.GetIncrementality().String(),
		EntityCount:         len(feed.Entity),
		UnknownFieldsBytes:  countUnknownFields(feed.ProtoReflect()),
		Alerts:              make([]AlertSummary, 0, len(feed.Entity)),
	}
	if feed.Header.Timestamp != nil {
		summary.Timestamp = timestamp(*feed.Header.Timestamp)
	}

	for _, entity := range feed.Entity {
		if entity.GetAlert() == nil {
			summary.NonAlertEntityCount++
			continue
		}
		summary.Alerts = append(summary.Alerts, summarizeAlert(entity))
	}
	summary.AlertCount = len(summary.Alerts)
	return summary, nil
}

func summarizeAlert(entity *gtfs.FeedEntity) AlertSummary {
	alert := entity.GetAlert()
	summary := AlertSummary{
		EntityID:           entity.GetId(),
		Deleted:            entity.GetIsDeleted(),
		Header:             translations(alert.GetHeaderText()),
		Description:        translations(alert.GetDescriptionText()),
		URL:                translations(alert.GetUrl()),
		UnknownFieldsBytes: countUnknownFields(entity.ProtoReflect()),
		UnknownFieldsHash:  unknownFieldsSHA256(entity.ProtoReflect()),
	}
	if alert.Cause != nil {
		value := alert.GetCause().String()
		summary.Cause = &value
	}
	if alert.Effect != nil {
		value := alert.GetEffect().String()
		summary.Effect = &value
	}
	if alert.SeverityLevel != nil {
		value := alert.GetSeverityLevel().String()
		summary.Severity = &value
	}

	for _, period := range alert.GetActivePeriod() {
		item := ActivePeriod{}
		if period.Start != nil {
			item.Start = timestamp(*period.Start)
		}
		if period.End != nil {
			item.End = timestamp(*period.End)
		}
		summary.ActivePeriods = append(summary.ActivePeriods, item)
	}
	for _, selector := range alert.GetInformedEntity() {
		item := InformedEntity{
			AgencyID:    selector.GetAgencyId(),
			RouteID:     selector.GetRouteId(),
			RouteType:   selector.RouteType,
			StopID:      selector.GetStopId(),
			DirectionID: selector.DirectionId,
		}
		if trip := selector.GetTrip(); trip != nil {
			item.TripID = trip.GetTripId()
			item.TripRouteID = trip.GetRouteId()
			item.TripStartTime = trip.GetStartTime()
			item.TripStartDate = trip.GetStartDate()
			item.TripDirection = trip.DirectionId
			if trip.ScheduleRelationship != nil {
				item.TripSchedule = trip.GetScheduleRelationship().String()
			}
		}
		summary.InformedEntities = append(summary.InformedEntities, item)
	}
	return summary
}

func translations(value *gtfs.TranslatedString) []Translation {
	if value == nil {
		return nil
	}
	result := make([]Translation, 0, len(value.Translation))
	for _, translation := range value.Translation {
		result = append(result, Translation{
			Text:     translation.GetText(),
			Language: translation.GetLanguage(),
		})
	}
	return result
}

func timestamp(value uint64) *Timestamp {
	result := &Timestamp{Unix: value}
	if value <= math.MaxInt64 {
		result.UTC = time.Unix(int64(value), 0).UTC().Format(time.RFC3339)
	}
	return result
}

func countUnknownFields(message protoreflect.Message) int {
	count := len(message.GetUnknown())
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsList() && field.Message() != nil:
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				count += countUnknownFields(list.Get(i).Message())
			}
		case field.IsMap() && field.MapValue().Message() != nil:
			value.Map().Range(func(_ protoreflect.MapKey, item protoreflect.Value) bool {
				count += countUnknownFields(item.Message())
				return true
			})
		case field.Message() != nil:
			count += countUnknownFields(value.Message())
		}
		return true
	})
	return count
}

func unknownFieldsSHA256(message protoreflect.Message) string {
	var records []string
	collectUnknownFields(message, string(message.Descriptor().FullName()), &records)
	if len(records) == 0 {
		return ""
	}
	sort.Strings(records)
	hash := sha256.New()
	for _, record := range records {
		hash.Write([]byte(record))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func collectUnknownFields(message protoreflect.Message, path string, records *[]string) {
	if unknown := message.GetUnknown(); len(unknown) > 0 {
		*records = append(*records, path+":"+hex.EncodeToString(unknown))
	}
	fields := message.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.Message() == nil || !message.Has(field) {
			continue
		}
		fieldPath := path + "." + strconv.Itoa(int(field.Number()))
		value := message.Get(field)
		switch {
		case field.IsList():
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				collectUnknownFields(list.Get(index).Message(), fieldPath+"[]", records)
			}
		case field.IsMap() && field.MapValue().Message() != nil:
			value.Map().Range(func(key protoreflect.MapKey, item protoreflect.Value) bool {
				collectUnknownFields(item.Message(), fieldPath+"["+key.String()+"]", records)
				return true
			})
		default:
			collectUnknownFields(value.Message(), fieldPath, records)
		}
	}
}
