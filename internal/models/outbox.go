package models

import "time"

type OutboxEvent struct {
	ID            string    `db:"id" json:"id"`
	AggregateType string    `db:"aggregate_type" json:"aggregate_type"`
	AggregateID   string    `db:"aggregate_id" json:"aggregate_id"`
	EventType     string    `db:"event_type" json:"event_type"`
	Payload       []byte    `db:"payload" json:"payload"` // JSONB stored as bytes
	Status        string    `db:"status" json:"status"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
}