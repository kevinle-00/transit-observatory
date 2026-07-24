package realtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

type canonicalAlert struct {
	Deleted            bool             `json:"deleted"`
	Cause              *string          `json:"cause"`
	Effect             *string          `json:"effect"`
	Severity           *string          `json:"severity"`
	Header             []Translation    `json:"header"`
	Description        []Translation    `json:"description"`
	URL                []Translation    `json:"url"`
	ActivePeriods      []ActivePeriod   `json:"active_periods"`
	InformedEntities   []InformedEntity `json:"informed_entities"`
	UnknownFieldsBytes int              `json:"unknown_fields_bytes"`
	UnknownFieldsHash  string           `json:"unknown_fields_sha256"`
}

func HashAlert(alert AlertSummary) (string, error) {
	if alert.UnknownFieldsBytes > 0 {
		return "", fmt.Errorf("alert contains %d unknown protobuf bytes; revision hashing requires explicit field support", alert.UnknownFieldsBytes)
	}
	canonical := canonicalAlert{
		Deleted:            alert.Deleted,
		Cause:              alert.Cause,
		Effect:             alert.Effect,
		Severity:           alert.Severity,
		Header:             append([]Translation{}, alert.Header...),
		Description:        append([]Translation{}, alert.Description...),
		URL:                append([]Translation{}, alert.URL...),
		ActivePeriods:      append([]ActivePeriod{}, alert.ActivePeriods...),
		InformedEntities:   append([]InformedEntity{}, alert.InformedEntities...),
		UnknownFieldsBytes: alert.UnknownFieldsBytes,
		UnknownFieldsHash:  alert.UnknownFieldsHash,
	}

	sortTranslations(canonical.Header)
	sortTranslations(canonical.Description)
	sortTranslations(canonical.URL)
	sort.Slice(canonical.ActivePeriods, func(i, j int) bool {
		return encodedLess(canonical.ActivePeriods[i], canonical.ActivePeriods[j])
	})
	sort.Slice(canonical.InformedEntities, func(i, j int) bool {
		return encodedLess(canonical.InformedEntities[i], canonical.InformedEntities[j])
	})

	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func sortTranslations(translations []Translation) {
	sort.Slice(translations, func(i, j int) bool {
		if translations[i].Language == translations[j].Language {
			return translations[i].Text < translations[j].Text
		}
		return translations[i].Language < translations[j].Language
	})
}

func encodedLess(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) < string(rightJSON)
}
