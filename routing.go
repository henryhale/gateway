package gateway

import (
	"context"
	"time"
)

// Candidate is an immutable snapshot of an eligible provider used for routing.
type Candidate struct {
	index           int
	id              ProviderID
	priority        int
	weight          uint32
	cost            float64
	observedLatency time.Duration
	inFlight        int64
	total           uint64
	failures        uint64
}

// ID returns the provider identifier.
func (c Candidate) ID() ProviderID { return c.id }

// Priority returns the provider's configured priority.
func (c Candidate) Priority() int { return c.priority }

// Weight returns the provider's configured routing weight.
func (c Candidate) Weight() uint32 { return c.weight }

// Cost returns the provider's application-defined normalized cost.
func (c Candidate) Cost() float64 { return c.cost }

// ObservedLatency returns the provider's locally observed EWMA latency.
func (c Candidate) ObservedLatency() time.Duration { return c.observedLatency }

// InFlight returns the number of locally active calls at selection time.
func (c Candidate) InFlight() int64 { return c.inFlight }

// FailureRate returns the provider's observed local failure ratio.
func (c Candidate) FailureRate() float64 {
	if c.total == 0 {
		return 0
	}
	return float64(c.failures) / float64(c.total)
}

// RoutingStrategy selects one index from the provided candidate slice.
//
// Implementations must be concurrency-safe and must not retain candidates after
// Select returns because the gateway may reuse the backing storage.
type RoutingStrategy interface {
	Select(context.Context, Request, []Candidate) (int, error)
}

// RoutingFunc adapts a function to the RoutingStrategy interface.
type RoutingFunc func(context.Context, Request, []Candidate) (int, error)

// Select delegates provider selection to the adapted function.
func (f RoutingFunc) Select(ctx context.Context, request Request, candidates []Candidate) (int, error) {
	return f(ctx, request, candidates)
}

// Scorer assigns a lower-is-better score to a routing candidate.
type Scorer interface {
	Score(Request, Candidate) float64
}

// ScoreFunc adapts a function to the Scorer interface.
type ScoreFunc func(Request, Candidate) float64

// Score delegates scoring to the adapted function.
func (f ScoreFunc) Score(request Request, candidate Candidate) float64 {
	return f(request, candidate)
}
