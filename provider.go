package gateway

import "context"

// Provider is the complete transport-independent provider contract.
type Provider[RequestPayload any, ResponsePayload any] interface {
	// Name returns the unique provider registration name.
	Name() string

	// Supports reports whether the provider can execute an operation.
	Supports(operation Operation) bool

	// Execute translates, sends, receives, and normalizes one request.
	Execute(
		ctx context.Context,
		request Request[RequestPayload],
	) (ResponsePayload, error)
}
