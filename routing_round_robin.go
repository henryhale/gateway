package gateway

import (
	"context"
	"errors"
	"sync/atomic"
)

type roundRobinStrategy struct{ counter atomic.Int64 }

// RoundRobin selects eligible providers in a concurrency-safe rotating order.
func RoundRobin() RoutingStrategy { return &roundRobinStrategy{} }

// Select returns the next round-robin candidate index.
func (s *roundRobinStrategy) Select(_ context.Context, _ Request, candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("gateway: round-robin routing received no candidates")
	}
	n := s.counter.Add(1) - 1
	return int(n % int64(len(candidates))), nil
}
