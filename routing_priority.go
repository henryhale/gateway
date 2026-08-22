package gateway

import (
	"context"
	"errors"
	"math"
)

// Priority selects the lowest configured priority, optionally honoring an explicit ID order.
func Priority(ids ...ProviderID) RoutingStrategy {
	order := make(map[ProviderID]int, len(ids))
	for i, id := range ids {
		if _, exists := order[id]; !exists {
			order[id] = i
		}
	}
	return RoutingFunc(func(_ context.Context, _ Request, candidates []Candidate) (int, error) {
		if len(candidates) == 0 {
			return -1, errors.New("gateway: priority routing received no candidates")
		}
		if len(order) > 0 {
			bestIndex := -1
			bestRank := math.MaxInt
			for i, candidate := range candidates {
				if rank, ok := order[candidate.id]; ok && rank < bestRank {
					bestIndex = i
					bestRank = rank
				}
			}
			if bestIndex >= 0 {
				return bestIndex, nil
			}
		}
		bestIndex := 0
		bestPriority := candidates[0].priority
		for i := 1; i < len(candidates); i++ {
			if candidates[i].priority < bestPriority {
				bestIndex = i
				bestPriority = candidates[i].priority
			}
		}
		return bestIndex, nil
	})
}
