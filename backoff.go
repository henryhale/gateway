package gateway

import "time"

// Backoff computes a delay before another provider attempt.
type Backoff interface {
	Delay(attempt int) time.Duration
}

// BackoffFunc adapts a function to the Backoff interface.
type BackoffFunc func(int) time.Duration

// Delay returns the delay produced by the adapted function.
func (f BackoffFunc) Delay(attempt int) time.Duration { return f(attempt) }

// NoBackoff returns a backoff policy that never delays.
func NoBackoff() Backoff {
	return BackoffFunc(func(int) time.Duration { return 0 })
}

// FixedBackoff returns a policy that always waits the same duration.
func FixedBackoff(delay time.Duration) Backoff {
	return BackoffFunc(func(int) time.Duration { return delay })
}

// ExponentialBackoff computes bounded exponential retry delays.
type ExponentialBackoff struct {
	Initial time.Duration
	Maximum time.Duration
}

// Delay returns the bounded exponential delay for an attempt number.
func (b ExponentialBackoff) Delay(attempt int) time.Duration {
	if b.Initial <= 0 || attempt <= 0 {
		return 0
	}
	delay := b.Initial
	for i := 1; i < attempt; i++ {
		if b.Maximum > 0 && delay >= b.Maximum/2 {
			return b.Maximum
		}
		delay *= 2
	}
	if b.Maximum > 0 && delay > b.Maximum {
		return b.Maximum
	}
	return delay
}
