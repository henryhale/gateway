package gateway

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// RoutingStrategy selects one provider from the currently eligible candidates.
type RoutingStrategy interface {
	// Name returns the strategy identifier used in diagnostics.
	Name() string

	// Select chooses one candidate or returns an error when none can be selected.
	Select(ctx context.Context, candidates []ProviderState) (ProviderState, error)
}

// CandidateScorer assigns a lower-is-better score to a provider candidate.
type CandidateScorer interface {
	// Score returns a lower-is-better score for a candidate.
	Score(candidate ProviderState) float64
}

type candidateScoreFunc func(candidate ProviderState) float64

// Score returns a lower-is-better score for a candidate.
func (f candidateScoreFunc) Score(candidate ProviderState) float64 {
	return f(candidate)
}

type priorityStrategy struct {
	order map[string]int
}

// Priority creates a strategy that follows the supplied provider order.
func Priority(providerNames ...string) RoutingStrategy {
	order := make(map[string]int, len(providerNames))
	for index, name := range providerNames {
		order[name] = index
	}
	return &priorityStrategy{order: order}
}

// Name returns the built-in strategy identifier.
func (s *priorityStrategy) Name() string {
	return "priority"
}

// Select chooses the candidate with the lowest configured priority.
func (s *priorityStrategy) Select(
	_ context.Context,
	candidates []ProviderState,
) (ProviderState, error) {
	if len(candidates) == 0 {
		return ProviderState{}, errors.New("gateway: priority routing: no candidates")
	}

	ordered := append([]ProviderState(nil), candidates...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		left, leftConfigured := s.order[ordered[i].Name]
		right, rightConfigured := s.order[ordered[j].Name]

		switch {
		case leftConfigured && rightConfigured:
			return left < right
		case leftConfigured:
			return true
		case rightConfigured:
			return false
		default:
			return ordered[i].Priority < ordered[j].Priority
		}
	})

	return ordered[0], nil
}

type roundRobinStrategy struct {
	mu   sync.Mutex
	next int
}

// RoundRobin creates a concurrency-safe round-robin routing strategy.
func RoundRobin() RoutingStrategy {
	return &roundRobinStrategy{}
}

// Name returns the built-in strategy identifier.
func (s *roundRobinStrategy) Name() string {
	return "round_robin"
}

// Select chooses the next candidate in a stable sorted rotation.
func (s *roundRobinStrategy) Select(
	_ context.Context,
	candidates []ProviderState,
) (ProviderState, error) {
	if len(candidates) == 0 {
		return ProviderState{}, errors.New("gateway: round-robin routing: no candidates")
	}

	ordered := append([]ProviderState(nil), candidates...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		return ordered[i].Name < ordered[j].Name
	})

	s.mu.Lock()
	selected := ordered[s.next%len(ordered)]
	s.next = (s.next + 1) % len(ordered)
	s.mu.Unlock()

	return selected, nil
}

type weightedStrategy struct {
	weights map[string]int
}

// Weighted creates a weighted-random routing strategy.
func Weighted(weights map[string]int) RoutingStrategy {
	copied := make(map[string]int, len(weights))
	for name, weight := range weights {
		copied[name] = weight
	}

	return &weightedStrategy{weights: copied}
}

// Name returns the built-in strategy identifier.
func (s *weightedStrategy) Name() string {
	return "weighted"
}

// Select chooses a provider according to configured or registered weights.
func (s *weightedStrategy) Select(
	_ context.Context,
	candidates []ProviderState,
) (ProviderState, error) {
	if len(candidates) == 0 {
		return ProviderState{}, errors.New("gateway: weighted routing: no candidates")
	}

	total := 0
	weights := make([]int, len(candidates))
	for index, candidate := range candidates {
		weight := s.weights[candidate.Name]
		if weight <= 0 {
			weight = candidate.Weight
		}
		if weight <= 0 {
			weight = 1
		}
		weights[index] = weight
		total += weight
	}

	target := randomIntn(total)

	for index, weight := range weights {
		if target < weight {
			return candidates[index], nil
		}
		target -= weight
	}

	return candidates[len(candidates)-1], nil
}

type powerOfTwoStrategy struct {
	scorer CandidateScorer
}

// PowerOfTwo creates a strategy that samples two candidates and chooses the better score.
func PowerOfTwo(scorer CandidateScorer) RoutingStrategy {
	if scorer == nil {
		scorer = ByObservedLatency()
	}

	return &powerOfTwoStrategy{scorer: scorer}
}

// Name returns the built-in strategy identifier.
func (s *powerOfTwoStrategy) Name() string {
	return "power_of_two"
}

// Select samples up to two candidates and returns the lower-scored provider.
func (s *powerOfTwoStrategy) Select(
	_ context.Context,
	candidates []ProviderState,
) (ProviderState, error) {
	if len(candidates) == 0 {
		return ProviderState{}, errors.New("gateway: power-of-two routing: no candidates")
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	firstIndex := randomIntn(len(candidates))
	secondIndex := randomIntn(len(candidates) - 1)
	if secondIndex >= firstIndex {
		secondIndex++
	}

	first := candidates[firstIndex]
	second := candidates[secondIndex]
	if s.scorer.Score(first) <= s.scorer.Score(second) {
		return first, nil
	}

	return second, nil
}

type lowestCostStrategy struct{}

// LowestCost creates a strategy that selects the least expensive provider.
func LowestCost() RoutingStrategy {
	return lowestCostStrategy{}
}

// Name returns the built-in strategy identifier.
func (lowestCostStrategy) Name() string {
	return "lowest_cost"
}

// Select chooses the candidate with the lowest configured cost.
func (lowestCostStrategy) Select(
	_ context.Context,
	candidates []ProviderState,
) (ProviderState, error) {
	if len(candidates) == 0 {
		return ProviderState{}, errors.New("gateway: lowest-cost routing: no candidates")
	}

	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Cost < selected.Cost {
			selected = candidate
		}
	}

	return selected, nil
}

// ByObservedLatency creates a scorer that prefers lower observed latency.
func ByObservedLatency() CandidateScorer {
	return candidateScoreFunc(func(candidate ProviderState) float64 {
		if candidate.ObservedLatency <= 0 {
			return float64(time.Second)
		}
		return float64(candidate.ObservedLatency)
	})
}

// ByCost creates a scorer that prefers lower configured cost.
func ByCost() CandidateScorer {
	return candidateScoreFunc(func(candidate ProviderState) float64 {
		return candidate.Cost
	})
}
