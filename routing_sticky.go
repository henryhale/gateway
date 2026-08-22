package gateway

import (
	"context"
	"errors"
	"hash/fnv"
)

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
