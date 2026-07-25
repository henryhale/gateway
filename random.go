package gateway

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
)

// randomFloat64 returns a cryptographically random number in [0, 1).
func randomFloat64() float64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0.5
	}
	// A float64 mantissa holds 53 bits; shifting keeps the result uniform in [0, 1).
	return float64(binary.BigEndian.Uint64(buf[:])>>11) / (1 << 53)
}

// randomIntn returns a uniform random number in [0, n) using crypto/rand.
func randomIntn(n int) int {
	if n <= 0 {
		panic("gateway: randomIntn: n must be positive")
	}

	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}

	return int(value.Int64())
}
