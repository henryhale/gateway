package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrorKind classifies failures independently of any provider or transport.
type ErrorKind string

const (
	// ErrorValidation indicates invalid application input.
	ErrorValidation ErrorKind = "validation"
	// ErrorAuthentication indicates invalid or missing provider credentials.
	ErrorAuthentication ErrorKind = "authentication"
	// ErrorAuthorization indicates insufficient provider permissions.
	ErrorAuthorization ErrorKind = "authorization"
	// ErrorRateLimited indicates provider throttling.
	ErrorRateLimited ErrorKind = "rate_limit"
	// ErrorTimeout indicates a provider or request deadline was exceeded.
	ErrorTimeout ErrorKind = "timeout"
	// ErrorUnavailable indicates a provider could not serve the request.
	ErrorUnavailable ErrorKind = "unavailable"
	// ErrorRejected indicates a provider rejected a valid operation.
	ErrorRejected ErrorKind = "rejected"
	// ErrorCanceled indicates the caller canceled the request.
	ErrorCanceled ErrorKind = "canceled"
	// ErrorInternal indicates a framework or adapter failure.
	ErrorInternal ErrorKind = "internal"
	// ErrorUnknown indicates an unclassified provider failure.
	ErrorUnknown ErrorKind = "unknown"
)

// ErrorCode identifies the specific reason behind a GatewayError, scoped within its Kind.
type ErrorCode string

const (
	// CodeAdapterError indicates an unclassified error returned by a provider adapter.
	CodeAdapterError ErrorCode = "adapter_error"
	// CodeNilGateway indicates a request was made on a nil Gateway.
	CodeNilGateway ErrorCode = "nil_gateway"
	// CodeOperationRequired indicates a request was submitted without an operation.
	CodeOperationRequired ErrorCode = "operation_required"
	// CodeProviderHintUnknown indicates a request specified an unregistered provider hint.
	CodeProviderHintUnknown ErrorCode = "provider_hint_unknown"
	// CodeProviderHintUnsupported indicates a request's provider hint does not support the operation.
	CodeProviderHintUnsupported ErrorCode = "provider_hint_unsupported"
	// CodeOperationUnsupported indicates no registered provider supports the requested operation.
	CodeOperationUnsupported ErrorCode = "operation_unsupported"
	// CodeRoutingFailed indicates the routing strategy failed to select a provider.
	CodeRoutingFailed ErrorCode = "routing_failed"
	// CodeRoutingUnknownProvider indicates the routing strategy selected an unregistered provider.
	CodeRoutingUnknownProvider ErrorCode = "routing_unknown_provider"
	// CodeRetryLoopExhausted indicates the provider retry loop ended without a result.
	CodeRetryLoopExhausted ErrorCode = "retry_loop_exhausted"
	// CodeRequestCanceled indicates the caller canceled the request context.
	CodeRequestCanceled ErrorCode = "request_canceled"
	// CodeRequestTimeout indicates the request context deadline was exceeded.
	CodeRequestTimeout ErrorCode = "request_timeout"
	// CodeNilCodec indicates an HTTP provider was configured without a codec.
	CodeNilCodec ErrorCode = "nil_codec"
	// CodeInvalidBaseURL indicates an HTTP provider was configured with an invalid base URL.
	CodeInvalidBaseURL ErrorCode = "invalid_base_url"
	// CodeResponseReadFailed indicates the HTTP provider response body could not be read.
	CodeResponseReadFailed ErrorCode = "response_read_failed"
	// CodeResponseTooLarge indicates the HTTP provider response exceeded the configured size limit.
	CodeResponseTooLarge ErrorCode = "response_too_large"
	// CodeTransportError indicates an unclassified HTTP transport failure.
	CodeTransportError ErrorCode = "transport_error"
	// CodeNetworkTimeout indicates a lower-level network operation timed out.
	CodeNetworkTimeout ErrorCode = "network_timeout"
)

// GatewayError is the normalized error returned by the framework.
type GatewayError struct {
	Kind         ErrorKind
	Code         ErrorCode
	Message      string
	Provider     string
	Operation    Operation
	Attempt      int
	StatusCode   int
	Retryable    bool
	Fallbackable bool
	Cause        error
}

// Error returns a stable human-readable gateway error message.
func (e *GatewayError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Provider == "" {
		return fmt.Sprintf("gateway: %s: %s", e.Kind, e.Message)
	}

	return fmt.Sprintf("gateway: provider %s: %s: %s", e.Provider, e.Kind, e.Message)
}

// Unwrap returns the underlying adapter or transport error.
func (e *GatewayError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// AsError extracts a GatewayError from an error chain.
func AsError(err error) (*GatewayError, bool) {
	if gatewayError, ok := errors.AsType[*GatewayError](err); ok {
		return gatewayError, true
	}

	return nil, false
}

// HTTPProviderError creates a normalized error from an HTTP status response.
func HTTPProviderError(statusCode int, code ErrorCode, message string) *GatewayError {
	gatewayError := &GatewayError{
		Kind:       classifyHTTPStatus(statusCode),
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}

	switch gatewayError.Kind {
	case ErrorRateLimited, ErrorTimeout, ErrorUnavailable:
		gatewayError.Retryable = true
		gatewayError.Fallbackable = true
	}

	return gatewayError
}

// classifyHTTPStatus maps an HTTP status code to a transport-independent error kind.
func classifyHTTPStatus(statusCode int) ErrorKind {
	switch {
	case statusCode == http.StatusBadRequest || statusCode == http.StatusConflict || statusCode == http.StatusUnprocessableEntity:
		return ErrorRejected
	case statusCode == http.StatusUnauthorized:
		return ErrorAuthentication
	case statusCode == http.StatusForbidden:
		return ErrorAuthorization
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout:
		return ErrorTimeout
	case statusCode == http.StatusTooManyRequests:
		return ErrorRateLimited
	case statusCode >= http.StatusInternalServerError:
		return ErrorUnavailable
	default:
		return ErrorUnknown
	}
}

// normalizeError adds request and provider context to an arbitrary error.
func normalizeError(err error, provider string, operation Operation, attempt int) *GatewayError {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return contextGatewayError(err, provider, operation, attempt)
	}
	if errors.Is(err, context.Canceled) {
		return contextGatewayError(err, provider, operation, attempt)
	}

	if gatewayError, ok := AsError(err); ok {
		copy := *gatewayError
		if copy.Provider == "" {
			copy.Provider = provider
		}
		if copy.Operation == "" {
			copy.Operation = operation
		}
		if copy.Attempt == 0 {
			copy.Attempt = attempt
		}
		return &copy
	}

	return &GatewayError{
		Kind:      ErrorInternal,
		Code:      CodeAdapterError,
		Message:   err.Error(),
		Provider:  provider,
		Operation: operation,
		Attempt:   attempt,
		Cause:     err,
	}
}
