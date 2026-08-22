package gateway

import (
	"context"
	"errors"
)

type leastStrategy struct{ scorer Scorer }

// Least selects the candidate with the smallest scorer value.
func Least(scorer Scorer) RoutingStrategy {
	if scorer == nil {
		scorer = ByInFlight()
	}
	return &leastStrategy{scorer: scorer}
}

// Select returns the lowest-scoring candidate index in a single O(n) scan.
func (s *leastStrategy) Select(_ context.Context, request Request, candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("gateway: least routing received no candidates")
	}
	bestIndex := 0
	bestScore := s.scorer.Score(request, candidates[0])
	for i := 1; i < len(candidates); i++ {
		score := s.scorer.Score(request, candidates[i])
		if score < bestScore {
			bestIndex = i
			bestScore = score
		}
	}
	return bestIndex, nil
}
