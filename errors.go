package gateway

import (
	"errors"
	"fmt"
)

// ErrorCode classifies errors produced by the gateway kernel itself.
type ErrorCode string

const (
	// CodeInvalidRequest indicates an invalid routing request.
	CodeInvalidRequest ErrorCode = "invalid_request"
	// CodeNoProvider indicates that no provider is currently eligible.
	CodeNoProvider ErrorCode = "no_provider"
	// CodeRoutingFailed indicates that a routing strategy could not select a candidate.
	CodeRoutingFailed ErrorCode = "routing_failed"
	// CodeProviderFailed indicates that a provider returned an error.
	CodeProviderFailed ErrorCode = "provider_failed"
	// CodeAttemptsExhausted indicates that the configured attempt budget was exhausted.
	CodeAttemptsExhausted ErrorCode = "attempts_exhausted"
	// CodeDeadlineExceeded indicates that the request deadline elapsed.
	CodeDeadlineExceeded ErrorCode = "deadline_exceeded"
	// CodeCanceled indicates that the request context was canceled.
	CodeCanceled ErrorCode = "canceled"
)

// Error describes a failure surfaced by Gateway.HandleRequest.
type Error struct {
	Code      ErrorCode
	Operation Operation
	Provider  ProviderID
	Attempt   int
	Cause     error
}

// Error returns a compact human-readable error description.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Provider != "" {
		return fmt.Sprintf("gateway: %s: provider=%s operation=%s: %v", e.Code, e.Provider, e.Operation, e.Cause)
	}
	return fmt.Sprintf("gateway: %s: operation=%s: %v", e.Code, e.Operation, e.Cause)
}

// Unwrap returns the underlying provider, routing, or context error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// AsError extracts a gateway Error from an error chain.
func AsError(err error) (*Error, bool) {
	var gatewayError *Error
	if !errors.As(err, &gatewayError) {
		return nil, false
	}
	return gatewayError, true
}
