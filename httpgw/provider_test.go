package httpgw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gateway "github.com/henryhale/gateway"
)

type testCodec struct{ baseURL string }

// Encode builds an operation-specific test request.
func (c testCodec) Encode(ctx context.Context, request gateway.Request) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+string(request.Operation()), nil)
}

// Decode translates an HTTP response using the original gateway request.
func (c testCodec) Decode(_ context.Context, request gateway.Request, response *http.Response) (any, error) {
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	return string(request.Operation()) + ":" + body["value"], nil
}

// TestProvider verifies request-aware bidirectional HTTP translation.
func TestProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"value": r.URL.Path[1:]})
	}))
	defer server.Close()
	provider, err := NewProvider(server.Client(), testCodec{baseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	value, err := provider.Handle(context.Background(), gateway.NewRequest("balance", struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	if value != "balance:balance" {
		t.Fatalf("value = %#v, want balance:balance", value)
	}
}

// TestForwardProvider verifies raw HTTP forwarding preserves streaming response ownership.
func TestForwardProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	provider, err := NewForwardProvider(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users/1", nil)
	value, err := provider.Handle(context.Background(), gateway.NewRequest("http.proxy", request))
	if err != nil {
		t.Fatal(err)
	}
	response := value.(*http.Response)
	defer response.Body.Close()
	if response.Header.Get("X-Upstream") != "yes" {
		t.Fatal("forward response did not come from upstream")
	}
}
