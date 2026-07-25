package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gw "github.com/henryhale/gateway"
)

type httpTestCodec struct{}

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
	if !ok || gatewayError.Code != "response_too_large" {
		t.Fatalf("expected response_too_large, got %v", err)
	}
}
