package gateway

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"math/big"
)

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
