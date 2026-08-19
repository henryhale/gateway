package gateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errTemporary = errors.New("temporary")

// TestGatewayRoutesByOperation verifies static operation filtering.
func TestGatewayRoutesByOperation(t *testing.T) {
	first := ProviderFunc(func(context.Context, Request) (any, error) { return "first", nil })
	second := ProviderFunc(func(context.Context, Request) (any, error) { return "second", nil })
	g, err := New(
		WithProviders(
			UseProvider("first", first, WithOperations("collect")),
			UseProvider("second", second, WithOperations("refund")),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.HandleRequest(context.Background(), NewRequest("refund", struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider() != "second" {
		t.Fatalf("provider = %q, want second", result.Provider())
	}
}

// TestGatewayFilter verifies dynamic pre-call eligibility filters.
func TestGatewayFilter(t *testing.T) {
	type payload struct{ Country string }
	filtered := ProviderFunc(func(context.Context, Request) (any, error) { return "filtered", nil })
	fallback := ProviderFunc(func(context.Context, Request) (any, error) { return "fallback", nil })
	countryFilter := FilterFunc(func(_ context.Context, request Request, _ Candidate) (bool, error) {
		value := request.Value().(payload)
		return value.Country == "UG", nil
	})
	g, err := New(WithProviders(
		UseProvider("filtered", filtered, WithFilter(countryFilter), WithProviderPriority(0)),
		UseProvider("fallback", fallback, WithProviderPriority(1)),
	))
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.HandleRequest(context.Background(), NewRequest("send", payload{Country: "KE"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider() != "fallback" {
		t.Fatalf("provider = %q, want fallback", result.Provider())
	}
}

// TestGatewayFailover verifies failed providers are excluded from a request chain.
func TestGatewayFailover(t *testing.T) {
	var firstCalls atomic.Int64
	first := ProviderFunc(func(context.Context, Request) (any, error) {
		firstCalls.Add(1)
		return nil, errTemporary
	})
	second := ProviderFunc(func(context.Context, Request) (any, error) { return "ok", nil })
	g, err := New(
		WithProviders(
			UseProvider("first", first, WithProviderPriority(0)),
			UseProvider("second", second, WithProviderPriority(1)),
		),
		WithFailurePolicy(FailoverWhen(func(err error) bool { return errors.Is(err, errTemporary) })),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.HandleRequest(context.Background(), NewRequest("send", nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider() != "second" {
		t.Fatalf("provider = %q, want second", result.Provider())
	}
	if firstCalls.Load() != 1 {
		t.Fatalf("first provider calls = %d, want 1", firstCalls.Load())
	}
}

// TestGatewayRetryThenFailover verifies bounded same-provider retries followed by failover.
func TestGatewayRetryThenFailover(t *testing.T) {
	var firstCalls atomic.Int64
	first := ProviderFunc(func(context.Context, Request) (any, error) {
		firstCalls.Add(1)
		return nil, errTemporary
	})
	second := ProviderFunc(func(context.Context, Request) (any, error) { return "ok", nil })
	g, err := New(
		WithProviders(
			UseProvider("first", first, WithProviderPriority(0)),
			UseProvider("second", second, WithProviderPriority(1)),
		),
		WithFailurePolicy(RetryThenFailover(2, NoBackoff(), func(err error) bool { return errors.Is(err, errTemporary) })),
		WithMaxAttempts(8),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.HandleRequest(context.Background(), NewRequest("send", nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider() != "second" {
		t.Fatalf("provider = %q, want second", result.Provider())
	}
	if firstCalls.Load() != 3 {
		t.Fatalf("first provider calls = %d, want 3", firstCalls.Load())
	}
}

// TestGatewayProviderHint verifies a hint is preferred on the first attempt.
func TestGatewayProviderHint(t *testing.T) {
	first := ProviderFunc(func(context.Context, Request) (any, error) { return "first", nil })
	second := ProviderFunc(func(context.Context, Request) (any, error) { return "second", nil })
	g, err := New(WithProviders(UseProvider("first", first), UseProvider("second", second)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.HandleRequest(context.Background(), NewRequest("send", nil, WithProviderHint("second")))
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider() != "second" {
		t.Fatalf("provider = %q, want second", result.Provider())
	}
}

// TestGatewayCooldown verifies repeated matching failures temporarily remove a provider.
func TestGatewayCooldown(t *testing.T) {
	bad := ProviderFunc(func(context.Context, Request) (any, error) { return nil, errTemporary })
	good := ProviderFunc(func(context.Context, Request) (any, error) { return "ok", nil })
	g, err := New(
		WithProviders(
			UseProvider("bad", bad,
				WithProviderPriority(0),
				WithCooldown(CooldownConfig{Failures: 1, Duration: time.Second, When: func(err error) bool { return errors.Is(err, errTemporary) }}),
			),
			UseProvider("good", good, WithProviderPriority(1)),
		),
		WithFailurePolicy(FailoverWhen(func(error) bool { return true })),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.HandleRequest(context.Background(), NewRequest("send", nil)); err != nil {
		t.Fatal(err)
	}
	result, err := g.HandleRequest(context.Background(), NewRequest("send", nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider() != "good" {
		t.Fatalf("provider = %q, want good while bad is cooling down", result.Provider())
	}
}

// TestGatewayTimeout verifies a configured timeout bounds provider execution.
func TestGatewayTimeout(t *testing.T) {
	slow := ProviderFunc(func(ctx context.Context, _ Request) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	g, err := New(WithProviders(UseProvider("slow", slow)), WithRequestTimeout(20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.HandleRequest(context.Background(), NewRequest("slow", nil))
	if err == nil {
		t.Fatal("expected deadline error")
	}
	gatewayError, ok := AsError(err)
	if !ok {
		t.Fatalf("error type = %T, want *gateway.Error", err)
	}
	if gatewayError.Code != CodeProviderFailed && gatewayError.Code != CodeDeadlineExceeded {
		t.Fatalf("code = %q, want provider_failed or deadline_exceeded", gatewayError.Code)
	}
}

// TestGatewayMaxInFlight verifies a provider's local bulkhead is never exceeded.
func TestGatewayMaxInFlight(t *testing.T) {
	var active atomic.Int64
	var maximum atomic.Int64
	provider := ProviderFunc(func(context.Context, Request) (any, error) {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		active.Add(-1)
		return "ok", nil
	})
	backup := ProviderFunc(func(context.Context, Request) (any, error) { return "backup", nil })
	g, err := New(WithProviders(
		UseProvider("limited", provider, WithMaxInFlight(2), WithProviderPriority(0)),
		UseProvider("backup", backup, WithProviderPriority(1)),
	))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = g.HandleRequest(context.Background(), NewRequest("send", nil))
		}()
	}
	wg.Wait()
	if maximum.Load() > 2 {
		t.Fatalf("max concurrent calls = %d, want <= 2", maximum.Load())
	}
}

// TestGatewayConcurrentUse verifies one gateway can serve many goroutines safely.
func TestGatewayConcurrentUse(t *testing.T) {
	var calls atomic.Int64
	provider := ProviderFunc(func(context.Context, Request) (any, error) {
		calls.Add(1)
		return 42, nil
	})
	g, err := New(WithProviders(UseProvider("provider", provider)), WithRouting(RoundRobin()))
	if err != nil {
		t.Fatal(err)
	}
	const requests = 1000
	var wg sync.WaitGroup
	wg.Add(requests)
	for i := 0; i < requests; i++ {
		go func() {
			defer wg.Done()
			result, requestErr := g.HandleRequest(context.Background(), NewRequest("read", i))
			if requestErr != nil {
				t.Errorf("HandleRequest: %v", requestErr)
				return
			}
			if value, ok := ValueAs[int](result); !ok || value != 42 {
				t.Errorf("result = %#v, want 42", result.Value())
			}
		}()
	}
	wg.Wait()
	if calls.Load() != requests {
		t.Fatalf("calls = %d, want %d", calls.Load(), requests)
	}
}

// TestGatewayOpaquePayload verifies gateway preserves payload identity.
func TestGatewayOpaquePayload(t *testing.T) {
	type payload struct{ Value int }
	input := &payload{Value: 7}
	provider := ProviderFunc(func(_ context.Context, request Request) (any, error) {
		if request.Value() != input {
			t.Fatal("gateway changed payload identity")
		}
		return request.Value(), nil
	})
	g, err := New(WithProviders(UseProvider("provider", provider)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := g.HandleRequest(context.Background(), NewRequest("echo", input))
	if err != nil {
		t.Fatal(err)
	}
	if result.Value() != input {
		t.Fatal("gateway changed response identity")
	}
}
