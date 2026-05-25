package replay

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/secrets"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type Record struct {
	ID             string `json:"id"`
	ObservationID  int64  `json:"observation_id"`
	Timestamp      int64  `json:"timestamp"`
	Actor          string `json:"actor"`
	Type           string `json:"type"`
	Tool           string `json:"tool"`
	PayloadPreview string `json:"payload_preview"`
	PayloadDetail  any    `json:"payload_detail"`
}

func Records(observations []store.Observation) []Record {
	records := make([]Record, 0, len(observations))
	for _, obs := range observations {
		payload := secrets.Redact(string(obs.PayloadJSON))
		var detail any
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			detail = payload
		}
		records = append(records, Record{
			ID:             fmt.Sprintf("observation:%d", obs.ID),
			ObservationID:  obs.ID,
			Timestamp:      obs.TS,
			Actor:          actor(obs.Kind, obs.Tool),
			Type:           obs.Kind,
			Tool:           obs.Tool,
			PayloadPreview: preview(payload, 240),
			PayloadDetail:  detail,
		})
	}
	return records
}

func actor(kind, tool string) string {
	switch {
	case kind == "user_message":
		return "user"
	case kind == "assistant_message" || kind == "summary":
		return "assistant"
	case tool != "" || strings.Contains(kind, "tool"):
		return "tool"
	default:
		return "system"
	}
}

func preview(payload string, maxRunes int) string {
	payload = strings.Join(strings.Fields(payload), " ")
	if utf8.RuneCountInString(payload) <= maxRunes {
		return payload
	}
	runes := []rune(payload)
	return string(runes[:maxRunes])
}
