package gateway_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gw "github.com/henryhale/gateway"
	"github.com/henryhale/gateway/internal/testprovider"
)

type testRequest struct {
	Value string
}

type testResponse struct {
	Value string
}

type fixedRoutingStrategy struct {
	provider string
}

// Name returns the test routing strategy name.
func (s fixedRoutingStrategy) Name() string {
	return "fixed"
}

// Select always returns the configured provider, regardless of the candidates.
func (s fixedRoutingStrategy) Select(
	_ context.Context,
	_ []gw.ProviderState,
) (gw.ProviderState, error) {
	return gw.ProviderState{Name: s.provider}, nil
}

// TestGatewayPriorityRouting verifies that priority routing selects the configured provider.
func TestGatewayPriorityRouting(t *testing.T) {
	primary := testprovider.New[testRequest, testResponse](
		"primary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "primary"}},
	)
	secondary := testprovider.New[testRequest, testResponse](
		"secondary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "secondary"}},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(
			gw.UseProvider(primary, gw.WithProviderPriority(1)),
			gw.UseProvider(secondary, gw.WithProviderPriority(2)),
		),
		gw.WithRouting(gw.Priority()),
	)
	if err != nil {
		t.Fatalf("construct gateway:  %v", err)
	}

	result, err := gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.execute",
		Payload:   testRequest{Value: "input"},
	})
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if result.Provider != "primary" {
		t.Fatalf("expected primary provider, got %q", result.Provider)
	}
	if result.Payload.Value != "primary" {
		t.Fatalf("expected primary response, got %q", result.Payload.Value)
	}
}

// TestGatewayFallback verifies that eligible failures move to another provider.
func TestGatewayFallback(t *testing.T) {
	primary := testprovider.New[testRequest, testResponse](
		"primary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Error: gw.HTTPProviderError(503, "down", "unavailable")},
	)
	secondary := testprovider.New[testRequest, testResponse](
		"secondary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "secondary"}},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(
			gw.UseProvider(primary, gw.WithProviderPriority(1)),
			gw.UseProvider(secondary, gw.WithProviderPriority(2)),
		),
		gw.WithRouting(gw.Priority()),
		gw.WithFallback(gw.FallbackOn(gw.ErrorUnavailable)),
	)
	if err != nil {
		t.Fatalf("construct gateway:  %v", err)
	}

	result, err := gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.execute",
	})
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if result.Provider != "secondary" {
		t.Fatalf("expected secondary provider, got %q", result.Provider)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("expected two attempts, got %d", len(result.Attempts))
	}
}

// TestGatewayRetry verifies that retryable failures are retried on the same provider.
func TestGatewayRetry(t *testing.T) {
	provider := testprovider.New[testRequest, testResponse](
		"provider",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Error: gw.HTTPProviderError(429, "limited", "retry")},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "ok"}},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(provider)),
		gw.WithRetry(gw.Retry{
			MaxAttempts: 2,
			Backoff:     gw.ExponentialBackoff{Initial: time.Millisecond, Maximum: time.Millisecond},
		}),
	)
	if err != nil {
		t.Fatalf("construct gateway:  %v", err)
	}

	result, err := gateway.HandleRequest(
		context.Background(),
		gw.Request[testRequest]{
			Operation: "test.execute",
		},
	)
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if provider.Calls() != 2 {
		t.Fatalf("expected two provider calls, got %d", provider.Calls())
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("expected two attempts, got %d", len(result.Attempts))
	}
}

// TestGatewayProviderHint verifies that a request can select a registered provider directly.
func TestGatewayProviderHint(t *testing.T) {
	primary := testprovider.New[testRequest, testResponse](
		"primary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "primary"}},
	)
	secondary := testprovider.New[testRequest, testResponse](
		"secondary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "secondary"}},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(primary), gw.UseProvider(secondary)),
	)
	if err != nil {
		t.Fatalf("construct gateway:  %v", err)
	}

	result, err := gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation:    "test.execute",
		ProviderHint: "secondary",
	})
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if result.Provider != "secondary" {
		t.Fatalf("expected secondary provider, got %q", result.Provider)
	}
}

// TestGatewayRejectsUnsupportedOperation verifies operation validation.
func TestGatewayRejectsUnsupportedOperation(t *testing.T) {
	provider := testprovider.New[testRequest, testResponse](
		"provider",
		[]gw.Operation{"test.execute"},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(provider)),
	)
	if err != nil {
		t.Fatalf("construct gateway:  %v", err)
	}

	_, err = gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.unsupported",
	})
	if err == nil {
		t.Fatal("expected unsupported operation error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok || gatewayError.Kind != gw.ErrorValidation {
		t.Fatalf("expected validation error, got %v", err)
	}
}

// TestGatewayRequestTimeout verifies the overall request deadline.
func TestGatewayRequestTimeout(t *testing.T) {
	provider := testprovider.New[testRequest, testResponse](
		"provider",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Delay: 50 * time.Millisecond},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(provider)),
		gw.WithRequestTimeout(5*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct gateway:  %v", err)
	}

	_, err = gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.execute",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok {
		t.Fatalf("expected gateway error, got %v", err)
	}
	if gatewayError.Kind != gw.ErrorTimeout {
		t.Fatalf("expected timeout error, got %s", gatewayError.Kind)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline cause, got %v", err)
	}
}

// TestNewRejectsDuplicateProviders verifies provider name uniqueness.
func TestNewRejectsDuplicateProviders(t *testing.T) {
	first := testprovider.New[testRequest, testResponse](
		"duplicate",
		[]gw.Operation{"test.execute"},
	)
	second := testprovider.New[testRequest, testResponse](
		"duplicate",
		[]gw.Operation{"test.execute"},
	)

	_, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(first), gw.UseProvider(second)),
	)
	if err == nil {
		t.Fatal("expected duplicate provider error")
	}
}

// TestGatewayRejectsIneligibleRoutingSelection verifies custom strategies cannot bypass eligibility.
func TestGatewayRejectsIneligibleRoutingSelection(t *testing.T) {
	eligible := testprovider.New[testRequest, testResponse](
		"eligible",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "eligible"}},
	)
	ineligible := testprovider.New[testRequest, testResponse](
		"ineligible",
		nil,
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "ineligible"}},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(eligible), gw.UseProvider(ineligible)),
		gw.WithRouting(fixedRoutingStrategy{provider: "ineligible"}),
	)
	if err != nil {
		t.Fatalf("construct gateway: %v", err)
	}

	_, err = gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.execute",
	})
	if err == nil {
		t.Fatal("expected ineligible routing selection error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok {
		t.Fatalf("expected gateway error, got %v", err)
	}
	if gatewayError.Code != gw.CodeRoutingIneligibleProvider {
		t.Fatalf("expected ineligible routing code, got %q", gatewayError.Code)
	}
	if eligible.Calls() != 0 || ineligible.Calls() != 0 {
		t.Fatalf(
			"expected no provider calls, got eligible=%d ineligible=%d",
			eligible.Calls(),
			ineligible.Calls(),
		)
	}
}

// TestGatewayRejectsUsedRoutingSelection verifies fallback cannot reselect an attempted provider.
func TestGatewayRejectsUsedRoutingSelection(t *testing.T) {
	primary := testprovider.New[testRequest, testResponse](
		"primary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Error: gw.HTTPProviderError(503, "down", "unavailable")},
	)
	secondary := testprovider.New[testRequest, testResponse](
		"secondary",
		[]gw.Operation{"test.execute"},
		testprovider.Outcome[testResponse]{Response: testResponse{Value: "secondary"}},
	)

	gateway, err := gw.New[testRequest, testResponse](
		gw.WithProviders(gw.UseProvider(primary), gw.UseProvider(secondary)),
		gw.WithRouting(fixedRoutingStrategy{provider: "primary"}),
		gw.WithFallback(gw.FallbackOn(gw.ErrorUnavailable)),
	)
	if err != nil {
		t.Fatalf("construct gateway: %v", err)
	}

	_, err = gateway.HandleRequest(context.Background(), gw.Request[testRequest]{
		Operation: "test.execute",
	})
	if err == nil {
		t.Fatal("expected used routing selection error")
	}

	gatewayError, ok := gw.AsError(err)
	if !ok {
		t.Fatalf("expected gateway error, got %v", err)
	}
	if gatewayError.Code != gw.CodeRoutingIneligibleProvider {
		t.Fatalf("expected ineligible routing code, got %q", gatewayError.Code)
	}
	if primary.Calls() != 1 || secondary.Calls() != 0 {
		t.Fatalf(
			"expected one primary and no secondary calls, got primary=%d secondary=%d",
			primary.Calls(),
			secondary.Calls(),
		)
	}
}
