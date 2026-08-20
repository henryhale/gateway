package gateway

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"hash/fnv"
	"math"
	"math/big"
	"sync/atomic"
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

type randomStrategy struct{}

// Random selects an eligible provider uniformly at random.
func Random() RoutingStrategy { return &randomStrategy{} }

// Select returns a uniformly selected candidate index.
func (s *randomStrategy) Select(_ context.Context, _ Request, candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("gateway: random routing received no candidates")
	}
	index, err := cryptoIndex(len(candidates))
	if err != nil {
		return -1, err
	}
	return index, nil
}

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

type stickyStrategy struct{}

// Sticky uses rendezvous hashing over Request.Key and provider IDs for affinity.
func Sticky() RoutingStrategy { return stickyStrategy{} }

// Select returns the highest rendezvous-hash candidate for the request key.
func (stickyStrategy) Select(_ context.Context, request Request, candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return -1, errors.New("gateway: sticky routing received no candidates")
	}
	if request.key == "" {
		return 0, nil
	}
	bestIndex := 0
	bestHash := rendezvousHash(request.key, candidates[0].id)
	for i := 1; i < len(candidates); i++ {
		hash := rendezvousHash(request.key, candidates[i].id)
		if hash > bestHash {
			bestIndex = i
			bestHash = hash
		}
	}
	return bestIndex, nil
}

// rendezvousHash computes a stable hash for an affinity key and provider ID.
func rendezvousHash(key string, id ProviderID) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

// cryptoIndex returns a crypto-random index in the range [0, limit).
func cryptoIndex(limit int) (int, error) {
	if limit <= 0 {
		return -1, errors.New("gateway: invalid random bound")
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return -1, err
	}
	return int(n.Int64()), nil
}

// cryptoUint64N returns a crypto-random uint64 in the range [0, limit).
func cryptoUint64N(limit uint64) (uint64, error) {
	if limit == 0 {
		return 0, errors.New("gateway: invalid random bound")
	}
	n, err := cryptorand.Int(cryptorand.Reader, new(big.Int).SetUint64(limit))
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// ByObservedLatency scores lower observed latency as better.
func ByObservedLatency() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return float64(candidate.observedLatency)
	})
}

// ByInFlight scores fewer concurrent provider calls as better.
func ByInFlight() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return float64(candidate.inFlight)
	})
}

// ByCost scores lower configured cost as better.
func ByCost() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return candidate.cost
	})
}

// ByFailureRate scores lower observed local failure ratio as better.
func ByFailureRate() Scorer {
	return ScoreFunc(func(_ Request, candidate Candidate) float64 {
		return candidate.FailureRate()
	})
}

// LowestLatency selects the provider with the lowest observed latency.
func LowestLatency() RoutingStrategy { return Least(ByObservedLatency()) }

// LeastBusy selects the provider with the fewest in-flight requests.
func LeastBusy() RoutingStrategy { return Least(ByInFlight()) }

// LowestCost selects the provider with the lowest configured cost.
func LowestCost() RoutingStrategy { return Least(ByCost()) }
