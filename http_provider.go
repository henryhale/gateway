package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Codec translates between the application's standard format and an HTTP provider format.
type Codec[RequestPayload any, ResponsePayload any] interface {
	// Supports reports whether the codec can translate an operation.
	Supports(operation Operation) bool

	// Encode translates a standard request into an HTTP provider request.
	Encode(
		ctx context.Context,
		request Request[RequestPayload],
	) (HTTPRequest, error)

	// Decode translates an HTTP provider response into the standard response.
	Decode(
		ctx context.Context,
		response HTTPResponse,
	) (ResponsePayload, error)
}

// HTTPRequest describes the provider-specific request produced by a Codec.
type HTTPRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
}

// HTTPResponse describes the raw provider response supplied to a Codec.
type HTTPResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// HTTPProviderConfig configures an HTTP-backed provider.
type HTTPProviderConfig struct {
	BaseURL          string
	Headers          http.Header
	Timeout          time.Duration
	MaxResponseBytes int64
	Client           *http.Client
}

type httpProvider[RequestPayload any, ResponsePayload any] struct {
	name             string
	baseURL          *url.URL
	headers          http.Header
	client           *http.Client
	maxResponseBytes int64
	codec            Codec[RequestPayload, ResponsePayload]
}

// NewHTTPProvider creates a transport-independent Provider backed by net/http.
func NewHTTPProvider[RequestPayload any, ResponsePayload any](
	name string,
	config HTTPProviderConfig,
	codec Codec[RequestPayload, ResponsePayload],
) Provider[RequestPayload, ResponsePayload] {
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		baseURL = &url.URL{}
	}

	client := config.Client
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}

	maxResponseBytes := config.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = 10 << 20
	}

	return &httpProvider[RequestPayload, ResponsePayload]{
		name:             name,
		baseURL:          baseURL,
		headers:          cloneHeader(config.Headers),
		client:           client,
		maxResponseBytes: maxResponseBytes,
		codec:            codec,
	}
}

// Name returns the provider registration name.
func (p *httpProvider[RequestPayload, ResponsePayload]) Name() string {
	return p.name
}

// Supports reports whether the provider codec supports an operation.
func (p *httpProvider[RequestPayload, ResponsePayload]) Supports(operation Operation) bool {
	return p.codec != nil && p.codec.Supports(operation)
}

// Execute performs one encoded HTTP exchange and decodes the provider response.
func (p *httpProvider[RequestPayload, ResponsePayload]) Execute(
	ctx context.Context,
	request Request[RequestPayload],
) (ResponsePayload, error) {
	var zero ResponsePayload

	if p.codec == nil {
		return zero, &GatewayError{
			Kind:     ErrorInternal,
			Code:     CodeNilCodec,
			Message:  "HTTP provider codec is nil",
			Provider: p.name,
		}
	}

	if p.baseURL == nil || p.baseURL.Scheme == "" || p.baseURL.Host == "" {
		return zero, &GatewayError{
			Kind:     ErrorValidation,
			Code:     CodeInvalidBaseURL,
			Message:  "HTTP provider base URL is invalid",
			Provider: p.name,
		}
	}

	encoded, err := p.codec.Encode(ctx, request)
	if err != nil {
		return zero, fmt.Errorf("encode provider request: %w", err)
	}

	httpRequest, err := p.buildRequest(ctx, request, encoded)
	if err != nil {
		return zero, err
	}

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return zero, classifyTransportError(p.name, err)
	}
	defer httpResponse.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, p.maxResponseBytes+1))
	if err != nil {
		return zero, &GatewayError{
			Kind:         ErrorUnavailable,
			Code:         CodeResponseReadFailed,
			Message:      "failed to read provider response",
			Provider:     p.name,
			Retryable:    true,
			Fallbackable: true,
			Cause:        err,
		}
	}

	if int64(len(body)) > p.maxResponseBytes {
		return zero, &GatewayError{
			Kind:     ErrorUnavailable,
			Code:     CodeResponseTooLarge,
			Message:  "provider response exceeded the configured size limit",
			Provider: p.name,
		}
	}

	decoded, err := p.codec.Decode(ctx, HTTPResponse{
		StatusCode: httpResponse.StatusCode,
		Headers:    cloneHeader(httpResponse.Header),
		Body:       body,
	})
	if err != nil {
		return zero, err
	}

	return decoded, nil
}

// buildRequest creates a concrete net/http request from the encoded provider request.
func (p *httpProvider[RequestPayload, ResponsePayload]) buildRequest(
	ctx context.Context,
	request Request[RequestPayload],
	encoded HTTPRequest,
) (*http.Request, error) {
	method := encoded.Method
	if method == "" {
		method = http.MethodPost
	}

	target := *p.baseURL
	if encoded.Path != "" {
		basePath := strings.TrimSuffix(target.Path, "/")
		requestPath := strings.TrimPrefix(encoded.Path, "/")
		target.Path = basePath + "/" + requestPath
	}
	if encoded.Query != nil {
		target.RawQuery = encoded.Query.Encode()
	}

	httpRequest, err := http.NewRequestWithContext(
		ctx,
		method,
		target.String(),
		bytes.NewReader(encoded.Body),
	)
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}

	httpRequest.Header = cloneHeader(p.headers)
	mergeHeader(httpRequest.Header, encoded.Headers)

	if request.ID != "" {
		httpRequest.Header.Set("X-Request-ID", request.ID)
	}
	if request.IdempotencyKey != "" {
		httpRequest.Header.Set("Idempotency-Key", request.IdempotencyKey)
	}

	return httpRequest, nil
}

// classifyTransportError converts net/http failures into normalized gateway errors.
func classifyTransportError(provider string, err error) error {
	kind := ErrorUnavailable
	code := CodeTransportError
	message := "provider transport failed"

	switch {
	case errors.Is(err, context.Canceled):
		kind = ErrorCanceled
		code = CodeRequestCanceled
		message = "request was canceled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = ErrorTimeout
		code = CodeRequestTimeout
		message = "provider request timed out"
	default:
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			kind = ErrorTimeout
			code = CodeNetworkTimeout
			message = "provider network operation timed out"
		}
	}

	gatewayError := &GatewayError{
		Kind:     kind,
		Code:     code,
		Message:  message,
		Provider: provider,
		Cause:    err,
	}

	if kind == ErrorTimeout || kind == ErrorUnavailable {
		gatewayError.Retryable = true
		gatewayError.Fallbackable = true
	}

	return gatewayError
}

// cloneHeader creates an independent copy of an HTTP header map.
func cloneHeader(source http.Header) http.Header {
	if source == nil {
		return make(http.Header)
	}

	return source.Clone()
}

// mergeHeader appends all source header values into the destination.
func mergeHeader(destination http.Header, source http.Header) {
	for key, values := range source {
		destination.Del(key)
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}
