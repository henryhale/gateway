package gateway

import (
	"context"
	"testing"
	"time"
)

// TestPriority verifies priority routing is a stable O(n) minimum scan.
func TestPriority(t *testing.T) {
	strategy := Priority()
	index, err := strategy.Select(context.Background(), Request{}, []Candidate{
		{id: "a", priority: 3},
		{id: "b", priority: 1},
		{id: "c", priority: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatalf("index = %d, want 1", index)
	}
}

// TestPriorityExplicitOrder verifies explicit provider ordering overrides static priority.
func TestPriorityExplicitOrder(t *testing.T) {
	strategy := Priority("c", "a")
	index, err := strategy.Select(context.Background(), Request{}, []Candidate{
		{id: "a", priority: 0},
		{id: "b", priority: -10},
		{id: "c", priority: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if index != 2 {
		t.Fatalf("index = %d, want 2", index)
	}
}

// TestLeast verifies generic scorer composition.
func TestLeast(t *testing.T) {
	strategy := Least(ByCost())
	index, err := strategy.Select(context.Background(), Request{}, []Candidate{
		{id: "a", cost: 4},
		{id: "b", cost: 1},
		{id: "c", cost: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatalf("index = %d, want 1", index)
	}
}

// TestPowerOfTwoSingleCandidate verifies the strategy handles the one-provider case.
func TestPowerOfTwoSingleCandidate(t *testing.T) {
	strategy := PowerOfTwo(ByObservedLatency())
	index, err := strategy.Select(context.Background(), Request{}, []Candidate{{id: "a", observedLatency: time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	if index != 0 {
		t.Fatalf("index = %d, want 0", index)
	}
}

// TestSticky verifies affinity is deterministic for a stable candidate set.
func TestSticky(t *testing.T) {
	strategy := Sticky()
	request := NewRequest("chat", nil, WithKey("session-123"))
	candidates := []Candidate{{id: "a"}, {id: "b"}, {id: "c"}}
	first, err := strategy.Select(context.Background(), request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		next, err := strategy.Select(context.Background(), request, candidates)
		if err != nil {
			t.Fatal(err)
		}
		if next != first {
			t.Fatalf("sticky index changed from %d to %d", first, next)
		}
	}
}
