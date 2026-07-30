package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gw "github.com/henryhale/gateway"
	"github.com/henryhale/gateway/internal/testprovider"
)

type httpTestCodec struct{}

type blockingRoundTripper struct {
	calls atomic.Int32
}

// RoundTrip blocks until the HTTP client timeout cancels the request.
func (t *blockingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	<-request.Context().Done()
	return nil, request.Context().Err()
}

// Supports reports whether the test codec supports the test operation.
func (httpTestCodec) Supports(operation gw.Operation) bool {
	return operation == "test.http"
}

// Encode creates the test provider request.
func (httpTestCodec) Encode(
	_ context.Context,
	request gw.Request[testRequest],
) (gw.HTTPRequest, error) {
	body, err := json.Marshal(request.Payload)
	if err != nil {
		return gw.HTTPRequest{}, err
	}

	return gw.HTTPRequest{
		Method: http.MethodPost,
		Path:   "/execute",
		Body:   body,
	}, nil
}

// Decode normalizes the test provider response.
func (httpTestCodec) Decode(
	_ context.Context,
	response gw.HTTPResponse,
) (testResponse, error) {
	if response.StatusCode != http.StatusOK {
		return testResponse{}, gw.HTTPProviderError(
			response.StatusCode,
			"provider_error",
			string(response.Body),
		)
	}

	var payload testResponse
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return testResponse{}, err
	}
	return payload, nil
}

// TestHTTPProviderTranslation verifies request and response translation.
func TestHTTPProviderTranslation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/execute" {
			t.Fatalf("expected /execute path, got %q", request.URL.Path)
		}
		if request.Header.Get("X-Request-ID") != "request-1" {
			t.Fatalf("missing propagated request ID")
		}
		if request.Header.Get("Idempotency-Key") != "idempotency-1" {
			t.Fatalf("missing propagated idempotency key")
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(testResponse{Value: "translated"})
	}))
	defer server.Close()

	provider := gw.NewHTTPProvider(
		"http-provider",
		gw.HTTPProviderConfig{BaseURL: server.URL},
		httpTestCodec{},
	)

	response, err := provider.Execute(context.Background(), gw.Request[testRequest]{
		ID:             "request-1",
		Operation:      "test.http",
		IdempotencyKey: "idempotency-1",
		Payload:        testRequest{Value: "input"},
	})
	if err != nil {
		t.Fatalf("execute provider: %v", err)
	}
	if response.Value != "translated" {
		t.Fatalf("expected translated response, got %q", response.Value)
	}
}

// TestHTTPProviderClassifiesStatus verifies provider HTTP error normalization.
func TestHTTPProviderClassifiesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	provider := gw.NewHTTPProvider(
		"http-provider",
		gw.HTTPProviderConfig{BaseURL: server.URL},
		httpTestCodec{},
	)

	_, err := provider.Execute(context.Background(), gw.Request[testRequest]{
		Operation: "test.http",
	})
	if err == nil {
		t.Fatal("expected provider error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok || gatewayError.Kind != gw.ErrorRateLimited {
		t.Fatalf("expected rate-limit error, got %v", err)
	}
}

// TestHTTPProviderResponseLimit verifies the configured response size limit.
func TestHTTPProviderResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("response larger than limit"))
	}))
	defer server.Close()

	provider := gw.NewHTTPProvider(
		"http-provider",
		gw.HTTPProviderConfig{
			BaseURL:          server.URL,
			MaxResponseBytes: 4,
		},
		httpTestCodec{},
	)

	_, err := provider.Execute(context.Background(), gw.Request[testRequest]{
		Operation: "test.http",
	})
	if err == nil {
		t.Fatal("expected response size error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok || gatewayError.Code != gw.CodeResponseTooLarge {
		t.Fatalf("expected response_too_large, got %v", err)
	}
}

// TestHTTPProviderTimeoutRetries verifies client timeouts retain their retryable classification.
func TestHTTPProviderTimeoutRetries(t *testing.T) {
	transport := &blockingRoundTripper{}
	provider := gw.NewHTTPProvider(
		"slow",
		gw.HTTPProviderConfig{
			BaseURL: "https://provider.example",
			Client: &http.Client{
				Timeout:   2 * time.Millisecond,
				Transport: transport,
			},
		},
		httpTestCodec{},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(provider)),
		gw.WithRetry(gw.Retry{MaxAttempts: 2}),
		gw.WithRequestTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct gateway: %v", err)
	}

	_, err = gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.http",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok {
		t.Fatalf("expected gateway error, got %v", err)
	}
	if gatewayError.Kind != gw.ErrorTimeout {
		t.Fatalf("expected timeout kind, got %q", gatewayError.Kind)
	}
	if !gatewayError.Retryable || !gatewayError.Fallbackable {
		t.Fatalf(
			"expected retryable and fallbackable timeout, got retryable=%t fallbackable=%t",
			gatewayError.Retryable,
			gatewayError.Fallbackable,
		)
	}
	if transport.calls.Load() != 2 {
		t.Fatalf("expected two HTTP attempts, got %d", transport.calls.Load())
	}
}

// TestGatewayTimeoutRemainsTerminal verifies the overall deadline is not retried as a provider timeout.
func TestGatewayTimeoutRemainsTerminal(t *testing.T) {
	transport := &blockingRoundTripper{}
	provider := gw.NewHTTPProvider(
		"slow",
		gw.HTTPProviderConfig{
			BaseURL: "https://provider.example",
			Client: &http.Client{
				Transport: transport,
			},
		},
		httpTestCodec{},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(provider)),
		gw.WithRetry(gw.Retry{MaxAttempts: 2}),
		gw.WithRequestTimeout(2*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct gateway: %v", err)
	}

	_, err = gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.http",
	})
	if err == nil {
		t.Fatal("expected gateway timeout error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok {
		t.Fatalf("expected gateway error, got %v", err)
	}
	if gatewayError.Kind != gw.ErrorTimeout || gatewayError.Code != gw.CodeRequestTimeout {
		t.Fatalf("expected request timeout, got kind=%q code=%q", gatewayError.Kind, gatewayError.Code)
	}
	if gatewayError.Retryable || gatewayError.Fallbackable {
		t.Fatalf(
			"expected terminal gateway timeout, got retryable=%t fallbackable=%t",
			gatewayError.Retryable,
			gatewayError.Fallbackable,
		)
	}
	if transport.calls.Load() != 1 {
		t.Fatalf("expected one HTTP attempt, got %d", transport.calls.Load())
	}
}

// TestHTTPProviderTimeoutFallsBack verifies client timeouts can move to another provider.
func TestHTTPProviderTimeoutFallsBack(t *testing.T) {
	transport := &blockingRoundTripper{}
	primary := gw.NewHTTPProvider(
		"slow",
		gw.HTTPProviderConfig{
			BaseURL: "https://provider.example",
			Client: &http.Client{
				Timeout:   2 * time.Millisecond,
				Transport: transport,
			},
		},
		httpTestCodec{},
	)
	secondary := testprovider.New[testRequest, testResponse](
		"secondary",
		[]gw.Operation{"test.http"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "fallback"}},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(primary), gw.UseProvider(secondary)),
		gw.WithRouting(gw.Priority("slow", "secondary")),
		gw.WithFallback(gw.FallbackOn(gw.ErrorTimeout)),
		gw.WithRequestTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct gateway: %v", err)
	}

	result, err := gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.http",
	})
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if result.Provider != "secondary" || result.Payload.Value != "fallback" {
		t.Fatalf(
			"expected secondary fallback response, got provider=%q payload=%q",
			result.Provider,
			result.Payload.Value,
		)
	}
	if transport.calls.Load() != 1 || secondary.Calls() != 1 {
		t.Fatalf(
			"expected one primary and one secondary call, got primary=%d secondary=%d",
			transport.calls.Load(),
			secondary.Calls(),
		)
	}
}
