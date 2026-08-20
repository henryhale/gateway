package gateway

import (
	"context"
	"errors"
)

type powerOfTwoStrategy struct {
	scorer Scorer
}

// PowerOfTwo samples two candidates and selects the lower-scoring one.
func PowerOfTwo(scorer Scorer) RoutingStrategy {
	if scorer == nil {
		scorer = ByInFlight()
	}
	return &powerOfTwoStrategy{scorer: scorer}
}

// Select returns the better of two randomly sampled candidates.
func (s *powerOfTwoStrategy) Select(_ context.Context, request Request, candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("gateway: power-of-two routing received no candidates")
	}
	if len(candidates) == 1 {
		return 0, nil
	}
	first, err := cryptoIndex(len(candidates))
	if err != nil {
		return -1, err
	}
	second, err := cryptoIndex(len(candidates) - 1)
	if err != nil {
		return -1, err
	}
	if second >= first {
		second++
	}
	if s.scorer.Score(request, candidates[second]) < s.scorer.Score(request, candidates[first]) {
		return second, nil
	}
	return first, nil
}
