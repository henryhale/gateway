package gateway

import (
	"errors"
	"time"
)

type config struct {
	providers   []ProviderRegistration
	routing     RoutingStrategy
	failure     FailurePolicy
	timeout     time.Duration
	maxAttempts int
	observer    Observer
}

// Option configures a Gateway at construction time.
type Option func(*config) error

// WithProviders registers one or more providers with the gateway.
func WithProviders(providers ...ProviderRegistration) Option {
	return func(config *config) error {
		config.providers = append(config.providers, providers...)
		return nil
	}
}

// WithRouting installs the routing strategy used for eligible providers.
func WithRouting(strategy RoutingStrategy) Option {
	return func(config *config) error {
		if strategy == nil {
			return errors.New("gateway: routing strategy cannot be nil")
		}
		config.routing = strategy
		return nil
	}
}

// WithFailurePolicy installs the explicit retry and failover policy.
func WithFailurePolicy(policy FailurePolicy) Option {
	return func(config *config) error {
		if policy == nil {
			return errors.New("gateway: failure policy cannot be nil")
		}
		config.failure = policy
		return nil
	}
}

// WithRequestTimeout applies a gateway-wide upper bound; zero disables it.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(config *config) error {
		if timeout < 0 {
			return errors.New("gateway: request timeout cannot be negative")
		}
		config.timeout = timeout
		return nil
	}
}

// WithMaxAttempts sets the hard total provider-attempt budget per request.
func WithMaxAttempts(maxAttempts int) Option {
	return func(config *config) error {
		if maxAttempts <= 0 {
			return errors.New("gateway: max attempts must be greater than zero")
		}
		config.maxAttempts = maxAttempts
		return nil
	}
}

// WithObserver enables optional synchronous gateway event observation.
func WithObserver(observer Observer) Option {
	return func(config *config) error {
		if observer == nil {
			return errors.New("gateway: observer cannot be nil")
		}
		config.observer = observer
		return nil
	}
}
