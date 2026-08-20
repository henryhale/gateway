package gateway

import (
	"context"
	"errors"
)

type weightedStrategy struct{}

// Weighted selects a provider in proportion to Candidate.Weight.
func Weighted() RoutingStrategy { return &weightedStrategy{} }

// Select returns a weighted-random candidate index.
func (s *weightedStrategy) Select(_ context.Context, _ Request, candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("gateway: weighted routing received no candidates")
	}
	var total uint64
	for _, candidate := range candidates {
		total += uint64(candidate.weight)
	}
	if total == 0 {
		index, err := cryptoIndex(len(candidates))
		if err != nil {
			return -1, err
		}
		return index, nil
	}
	pick, err := cryptoUint64N(total)
	if err != nil {
		return -1, err
	}
	var running uint64
	for i, candidate := range candidates {
		running += uint64(candidate.weight)
		if pick < running {
			return i, nil
		}
	}
	return len(candidates) - 1, nil
}
