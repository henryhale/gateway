package gateway

import (
	"context"
	"time"
)

// FailureAction determines how execution continues after a provider error.
type FailureAction uint8

const (
	// Stop terminates the request immediately.
	Stop FailureAction = iota
	// RetryProvider retries the same provider.
	RetryProvider
	// NextProvider excludes the failed provider and routes to another candidate.
	NextProvider
)

// Failure describes one unsuccessful provider invocation.
type Failure struct {
	Request         Request
	Provider        ProviderID
	Error           error
	Attempt         int
	ProviderAttempt int
}

// FailureDecision is the bounded action selected by a FailurePolicy.
type FailureDecision struct {
	Action FailureAction
	Delay  time.Duration
}

// FailurePolicy decides what the gateway should do after a provider error.
//
// Implementations must be safe for concurrent use.
type FailurePolicy interface {
	Decide(context.Context, Failure) FailureDecision
}

// FailurePolicyFunc adapts a function to the FailurePolicy interface.
type FailurePolicyFunc func(context.Context, Failure) FailureDecision

// Decide returns the decision produced by the adapted function.
func (f FailurePolicyFunc) Decide(ctx context.Context, failure Failure) FailureDecision {
	return f(ctx, failure)
}

// StopOnFailure returns the safe default policy that never replays requests.
func StopOnFailure() FailurePolicy {
	return FailurePolicyFunc(func(context.Context, Failure) FailureDecision {
		return FailureDecision{Action: Stop}
	})
}

// FailoverWhen moves to another provider when predicate accepts the error.
func FailoverWhen(predicate func(error) bool) FailurePolicy {
	return FailurePolicyFunc(func(_ context.Context, failure Failure) FailureDecision {
		if predicate != nil && predicate(failure.Error) {
			return FailureDecision{Action: NextProvider}
		}
		return FailureDecision{Action: Stop}
	})
}

// RetryThenFailover retries each provider before moving to another provider.
func RetryThenFailover(maxRetries int, backoff Backoff, predicate func(error) bool) FailurePolicy {
	if backoff == nil {
		backoff = NoBackoff()
	}
	return FailurePolicyFunc(func(_ context.Context, failure Failure) FailureDecision {
		if predicate == nil || !predicate(failure.Error) {
			return FailureDecision{Action: Stop}
		}
		if failure.ProviderAttempt <= maxRetries {
			return FailureDecision{
				Action: RetryProvider,
				Delay:  backoff.Delay(failure.ProviderAttempt),
			}
		}
		return FailureDecision{Action: NextProvider}
	})
}
