package gateway

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// BackoffPolicy calculates the delay before a retry attempt.
type BackoffPolicy interface {
	// Delay returns the wait duration before the supplied retry number.
	Delay(retryNumber int) time.Duration
}

// ExponentialBackoff implements capped exponential retry delays with optional jitter.
type ExponentialBackoff struct {
	Initial time.Duration
	Maximum time.Duration
	Jitter  float64
}

// Delay returns the capped exponential delay for a retry number.
func (b ExponentialBackoff) Delay(retryNumber int) time.Duration {
	if retryNumber <= 0 {
		return 0
	}

	initial := b.Initial
	if initial <= 0 {
		initial = 100 * time.Millisecond
	}
	maximum := b.Maximum
	if maximum <= 0 {
		maximum = 5 * time.Second
	}

	factor := math.Pow(2, float64(retryNumber-1))
	delay := time.Duration(float64(initial) * factor)
	delay = min(maximum, delay)

	if b.Jitter > 0 {
		jitter := b.Jitter
		if jitter > 1 {
			jitter = 1
		}
		multiplier := 1 - jitter + rand.Float64()*(2*jitter)
		delay = time.Duration(float64(delay) * multiplier)
	}

	return delay
}

// Retry configures same-provider retry behavior.
type Retry struct {
	MaxAttempts int
	Backoff     BackoffPolicy
}

// FallbackPolicy decides whether an error may be retried on another provider.
type FallbackPolicy interface {
	// Allows reports whether fallback is permitted for an error.
	Allows(err *GatewayError) bool
}

type errorFallbackPolicy struct {
	allowed map[ErrorKind]struct{}
}

// FallbackOn creates a policy that allows fallback for selected error kinds.
func FallbackOn(kinds ...ErrorKind) FallbackPolicy {
	allowed := make(map[ErrorKind]struct{}, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = struct{}{}
	}
	return errorFallbackPolicy{allowed: allowed}
}

// Allows reports whether the error kind is configured for fallback.
func (p errorFallbackPolicy) Allows(err *GatewayError) bool {
	if err == nil {
		return false
	}
	_, ok := p.allowed[err.Kind]
	return ok && err.Fallbackable
}

// waitForBackoff waits for a retry delay or exits when the context ends.
func waitForBackoff(ctx context.Context, backoff BackoffPolicy, retryNumber int) error {
	if backoff == nil {
		return nil
	}

	delay := backoff.Delay(retryNumber)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
