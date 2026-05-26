// Package audit defines persisted operator and automation events.
package audit

import "time"

// Event records an operator action or automated daemon observation.
type Event struct {
	// ID is the event identifier.
	ID string
	// Type is the event type.
	Type string
	// ActorEmail is the operator identity, if any.
	ActorEmail string
	// Message is a short event summary.
	Message string
	// CreatedAt is when the event happened.
	CreatedAt time.Time
}
