package realtime

import (
	"os"
	"testing"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestDecodeAlerts(t *testing.T) {
	payload := fixturePayload(t)

	summary, err := DecodeAlerts(payload)
	if err != nil {
		t.Fatalf("DecodeAlerts() error = %v", err)
	}
	if summary.GTFSRealtimeVersion != "2.0" {
		t.Errorf("GTFSRealtimeVersion = %q, want 2.0", summary.GTFSRealtimeVersion)
	}
	if summary.Timestamp == nil || summary.Timestamp.UTC != "2025-07-23T04:00:00Z" {
		t.Errorf("Timestamp = %#v, want 2025-07-23T04:00:00Z", summary.Timestamp)
	}
	if summary.EntityCount != 2 || summary.AlertCount != 1 || summary.NonAlertEntityCount != 1 {
		t.Errorf("counts = entities %d, alerts %d, non-alerts %d", summary.EntityCount, summary.AlertCount, summary.NonAlertEntityCount)
	}

	alert := summary.Alerts[0]
	if alert.EntityID != "metro-alert-123" {
		t.Errorf("EntityID = %q, want metro-alert-123", alert.EntityID)
	}
	if alert.Cause == nil || *alert.Cause != "TECHNICAL_PROBLEM" {
		t.Errorf("Cause = %v, want TECHNICAL_PROBLEM", alert.Cause)
	}
	if alert.Effect == nil || *alert.Effect != "SIGNIFICANT_DELAYS" {
		t.Errorf("Effect = %v, want SIGNIFICANT_DELAYS", alert.Effect)
	}
	if len(alert.Header) != 1 || alert.Header[0].Text != "Delays on the Alamein Line" {
		t.Errorf("Header = %#v", alert.Header)
	}
	if len(alert.InformedEntities) != 1 || alert.InformedEntities[0].RouteID != "2-ALM-bus" {
		t.Errorf("InformedEntities = %#v", alert.InformedEntities)
	}
	if alert.InformedEntities[0].DirectionID == nil || *alert.InformedEntities[0].DirectionID != 0 {
		t.Errorf("DirectionID = %v, want present value 0", alert.InformedEntities[0].DirectionID)
	}
}

func TestDecodeAlertsReportsUnknownFields(t *testing.T) {
	feed := fixtureFeed(t)
	feed.Entity[0].Alert.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	payload, err := proto.Marshal(feed)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	summary, err := DecodeAlerts(payload)
	if err != nil {
		t.Fatalf("DecodeAlerts() error = %v", err)
	}
	if summary.UnknownFieldsBytes != 3 || summary.Alerts[0].UnknownFieldsBytes != 3 {
		t.Errorf("unknown byte counts = feed %d, alert %d; want 3, 3", summary.UnknownFieldsBytes, summary.Alerts[0].UnknownFieldsBytes)
	}
	if summary.Alerts[0].UnknownFieldsHash == "" {
		t.Error("alert unknown fields hash is empty")
	}
}

func TestDecodeAlertsHashesUnknownFieldContent(t *testing.T) {
	firstFeed := fixtureFeed(t)
	firstFeed.Entity[0].Alert.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	firstPayload, err := proto.Marshal(firstFeed)
	if err != nil {
		t.Fatalf("marshal first fixture: %v", err)
	}
	secondFeed := fixtureFeed(t)
	secondFeed.Entity[0].Alert.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x02})
	secondPayload, err := proto.Marshal(secondFeed)
	if err != nil {
		t.Fatalf("marshal second fixture: %v", err)
	}

	first, err := DecodeAlerts(firstPayload)
	if err != nil {
		t.Fatalf("DecodeAlerts(first) error = %v", err)
	}
	second, err := DecodeAlerts(secondPayload)
	if err != nil {
		t.Fatalf("DecodeAlerts(second) error = %v", err)
	}
	if first.Alerts[0].UnknownFieldsHash == second.Alerts[0].UnknownFieldsHash {
		t.Errorf("different unknown field values have the same hash: %s", first.Alerts[0].UnknownFieldsHash)
	}
}

func fixturePayload(t *testing.T) []byte {
	t.Helper()
	payload, err := proto.Marshal(fixtureFeed(t))
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return payload
}

func fixtureFeed(t *testing.T) *gtfs.FeedMessage {
	t.Helper()
	data, err := os.ReadFile("testdata/service-alerts.textproto")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	feed := new(gtfs.FeedMessage)
	if err := prototext.Unmarshal(data, feed); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return feed
}
