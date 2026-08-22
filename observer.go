package gateway

import (
	"context"
	"time"
)

// EventType identifies an observability event emitted by the gateway.
type EventType uint8

const (
	// EventRequestStarted is emitted before routing starts.
	EventRequestStarted EventType = iota + 1
	// EventProviderSelected is emitted after a provider is selected.
	EventProviderSelected
	// EventAttemptFinished is emitted after each provider invocation.
	EventAttemptFinished
	// EventRequestFinished is emitted after a successful request.
	EventRequestFinished
)

// Event contains low-cardinality routing telemetry and never contains payloads.
type Event struct {
	Type      EventType
	RequestID string
	Operation Operation
	Provider  ProviderID
	Attempt   int
	Duration  time.Duration
	Error     error
}

// Observer receives optional gateway events.
//
// Implementations must be concurrency-safe and should return quickly.
type Observer interface {
	Observe(context.Context, Event)
}

// ObserverFunc adapts a function to the Observer interface.
type ObserverFunc func(context.Context, Event)

// Observe dispatches an event to the adapted function.
func (f ObserverFunc) Observe(ctx context.Context, event Event) { f(ctx, event) }
