package event

import (
	"github.com/google/uuid"
	"time"
)

// Envelope is the single JSON format used by every Kafka producer.
type Envelope struct {
	EventID      uuid.UUID `json:"event_id"`
	EventType    string    `json:"event_type"`
	EventVersion int       `json:"event_version"`
	AggregateID  uuid.UUID `json:"aggregate_id"`
	OccurredAt   time.Time `json:"occurred_at"`
	Producer     string    `json:"producer"`
	Data         any       `json:"data"`
}
