package gateway_test

import (
	"context"
	"testing"
	"time"

	gw "github.com/henryhale/gateway"
)

// TestPriorityStrategyUsesConfiguredOrder verifies explicit priority ordering.
func TestPriorityStrategyUsesConfiguredOrder(t *testing.T) {
	strategy := gw.Priority("second", "first")
	selected, err := strategy.Select(context.Background(), []gw.ProviderState{
		{Name: "first", Priority: 1},
		{Name: "second", Priority: 2},
	})
	if err != nil {
		t.Fatalf("select provider: %v", err)
	}
	if selected.Name != "second" {
		t.Fatalf("expected second, got %q", selected.Name)
	}
}

// TestRoundRobinStrategyRotates verifies stable provider rotation.
func TestRoundRobinStrategyRotates(t *testing.T) {
	strategy := gw.RoundRobin()
	candidates := []gw.ProviderState{{Name: "b"}, {Name: "a"}}

	first, err := strategy.Select(context.Background(), candidates)
	if err != nil {
		t.Fatalf("first selection: %v", err)
	}
	second, err := strategy.Select(context.Background(), candidates)
	if err != nil {
		t.Fatalf("second selection: %v", err)
	}

	if first.Name != "a" || second.Name != "b" {
		t.Fatalf("unexpected rotation: %q then %q", first.Name, second.Name)
	}
}

// TestLowestCostStrategySelectsCheapest verifies cost routing.
func TestLowestCostStrategySelectsCheapest(t *testing.T) {
	strategy := gw.LowestCost()
	selected, err := strategy.Select(context.Background(), []gw.ProviderState{
		{Name: "expensive", Cost: 2.0},
		{Name: "cheap", Cost: 1.0},
	})
	if err != nil {
		t.Fatalf("select provider: %v", err)
	}
	if selected.Name != "cheap" {
		t.Fatalf("expected cheap provider, got %q", selected.Name)
	}
}

// TestObservedLatencyScorerPrefersKnownFastProvider verifies latency scoring.
func TestObservedLatencyScorerPrefersKnownFastProvider(t *testing.T) {
	scorer := gw.ByObservedLatency()
	fast := scorer.Score(gw.ProviderState{ObservedLatency: 10 * time.Millisecond})
	slow := scorer.Score(gw.ProviderState{ObservedLatency: 50 * time.Millisecond})
	if fast >= slow {
		t.Fatalf("expected fast score below slow score: %f >= %f", fast, slow)
	}
}
