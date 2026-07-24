package realtime

import "testing"

func TestHashAlertIgnoresCollectionOrdering(t *testing.T) {
	directionZero := uint32(0)
	directionOne := uint32(1)
	first := AlertSummary{
		EntityID: "alert-1",
		Header: []Translation{
			{Text: "Travelling now", Language: "en"},
			{Text: "Voyage", Language: "fr"},
		},
		ActivePeriods: []ActivePeriod{
			{Start: &Timestamp{Unix: 200}, End: &Timestamp{Unix: 300}},
			{Start: &Timestamp{Unix: 100}, End: &Timestamp{Unix: 150}},
		},
		InformedEntities: []InformedEntity{
			{RouteID: "route-b", DirectionID: &directionOne},
			{RouteID: "route-a", DirectionID: &directionZero},
		},
	}
	second := AlertSummary{
		EntityID: "different-source-id",
		Header: []Translation{
			{Text: "Voyage", Language: "fr"},
			{Text: "Travelling now", Language: "en"},
		},
		ActivePeriods: []ActivePeriod{
			{Start: &Timestamp{Unix: 100}, End: &Timestamp{Unix: 150}},
			{Start: &Timestamp{Unix: 200}, End: &Timestamp{Unix: 300}},
		},
		InformedEntities: []InformedEntity{
			{RouteID: "route-a", DirectionID: &directionZero},
			{RouteID: "route-b", DirectionID: &directionOne},
		},
	}

	firstHash, err := HashAlert(first)
	if err != nil {
		t.Fatalf("HashAlert(first) error = %v", err)
	}
	secondHash, err := HashAlert(second)
	if err != nil {
		t.Fatalf("HashAlert(second) error = %v", err)
	}
	if firstHash != secondHash {
		t.Errorf("equivalent alerts have different hashes: %s != %s", firstHash, secondHash)
	}
}

func TestHashAlertDetectsContentChanges(t *testing.T) {
	first := AlertSummary{Description: []Translation{{Text: "Ten minute delay", Language: "en"}}}
	second := AlertSummary{Description: []Translation{{Text: "Twenty minute delay", Language: "en"}}}

	firstHash, err := HashAlert(first)
	if err != nil {
		t.Fatalf("HashAlert(first) error = %v", err)
	}
	secondHash, err := HashAlert(second)
	if err != nil {
		t.Fatalf("HashAlert(second) error = %v", err)
	}
	if firstHash == secondHash {
		t.Errorf("changed alerts have the same hash: %s", firstHash)
	}
}

func TestHashAlertPreservesOptionalZero(t *testing.T) {
	direction := uint32(0)
	missing := AlertSummary{InformedEntities: []InformedEntity{{RouteID: "route-a"}}}
	present := AlertSummary{InformedEntities: []InformedEntity{{RouteID: "route-a", DirectionID: &direction}}}

	missingHash, _ := HashAlert(missing)
	presentHash, _ := HashAlert(present)
	if missingHash == presentHash {
		t.Errorf("missing and explicit zero direction have the same hash: %s", missingHash)
	}
}

func TestHashAlertRejectsUnknownProtobufFields(t *testing.T) {
	_, err := HashAlert(AlertSummary{UnknownFieldsBytes: 3, UnknownFieldsHash: "abc"})
	if err == nil {
		t.Fatal("HashAlert() error = nil, want unsupported unknown-field error")
	}
}
