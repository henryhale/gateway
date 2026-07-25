package gateway

import (
	"errors"
	"log/slog"
	"maps"
	"time"
)

// Option configures a Gateway during construction.
type Option interface {
	apply(config *gatewayConfig) error
}

type optionFunc func(config *gatewayConfig) error

// apply applies a functional option to the gateway configuration.
func (f optionFunc) apply(config *gatewayConfig) error {
	return f(config)
}

type gatewayConfig struct {
	providers      []any
	routing        RoutingStrategy
	fallback       FallbackPolicy
	retry          Retry
	requestTimeout time.Duration
	logger         *slog.Logger
}

// ProviderRegistration binds a provider to routing metadata.
type ProviderRegistration[RequestPayload any, ResponsePayload any] struct {
	provider Provider[RequestPayload, ResponsePayload]
	settings providerSettings
}

type providerSettings struct {
	priority int
	weight   int
	cost     float64
	metadata map[string]string
}

// ProviderOption configures routing metadata for one provider registration.
type ProviderOption interface {
	applyProvider(settings *providerSettings)
}

type providerOptionFunc func(settings *providerSettings)

// applyProvider applies one provider registration option.
func (f providerOptionFunc) applyProvider(settings *providerSettings) {
	f(settings)
}

// UseProvider prepares a provider for registration with a Gateway.
func UseProvider[RequestPayload any, ResponsePayload any](
	provider Provider[RequestPayload, ResponsePayload],
	options ...ProviderOption,
) ProviderRegistration[RequestPayload, ResponsePayload] {
	settings := providerSettings{
		priority: 100,
		weight:   1,
		metadata: make(map[string]string),
	}

	for _, option := range options {
		if option != nil {
			option.applyProvider(&settings)
		}
	}

	return ProviderRegistration[RequestPayload, ResponsePayload]{
		provider: provider,
		settings: settings,
	}
}

// WithProviderPriority assigns a lower-is-preferred priority to a provider.
func WithProviderPriority(priority int) ProviderOption {
	return providerOptionFunc(func(settings *providerSettings) {
		settings.priority = priority
	})
}

// WithProviderWeight assigns a relative weight to a provider.
func WithProviderWeight(weight int) ProviderOption {
	return providerOptionFunc(func(settings *providerSettings) {
		settings.weight = weight
	})
}

// WithProviderCost assigns a normalized per-request cost used by cost routing.
func WithProviderCost(cost float64) ProviderOption {
	return providerOptionFunc(func(settings *providerSettings) {
		settings.cost = cost
	})
}

// WithProviderMetadata adds immutable routing metadata to a provider.
func WithProviderMetadata(metadata map[string]string) ProviderOption {
	return providerOptionFunc(func(settings *providerSettings) {
		settings.metadata = cloneStringMap(metadata)
	})
}

// WithProviders registers one or more typed providers with a Gateway.
func WithProviders[RequestPayload any, ResponsePayload any](
	providers ...ProviderRegistration[RequestPayload, ResponsePayload],
) Option {
	return optionFunc(func(config *gatewayConfig) error {
		for _, provider := range providers {
			config.providers = append(config.providers, provider)
		}
		return nil
	})
}

// WithRouting selects the routing strategy used by a Gateway.
func WithRouting(strategy RoutingStrategy) Option {
	return optionFunc(func(config *gatewayConfig) error {
		if strategy == nil {
			return errors.New("gateway: routing strategy cannot be nil")
		}
		config.routing = strategy
		return nil
	})
}

// WithFallback selects the cross-provider fallback policy.
func WithFallback(policy FallbackPolicy) Option {
	return optionFunc(func(config *gatewayConfig) error {
		if policy == nil {
			return errors.New("gateway: fallback policy cannot be nil")
		}
		config.fallback = policy
		return nil
	})
}

// WithRetry configures same-provider retry behavior.
func WithRetry(retry Retry) Option {
	return optionFunc(func(config *gatewayConfig) error {
		if retry.MaxAttempts < 1 {
			return errors.New("gateway: retry max attempts must be at least one")
		}
		config.retry = retry
		return nil
	})
}

// WithRequestTimeout sets the maximum duration for one HandleRequest call.
func WithRequestTimeout(timeout time.Duration) Option {
	return optionFunc(func(config *gatewayConfig) error {
		if timeout <= 0 {
			return errors.New("gateway: request timeout must be positive")
		}
		config.requestTimeout = timeout
		return nil
	})
}

// WithLogger installs a custom structured logger.
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(config *gatewayConfig) error {
		if logger == nil {
			return errors.New("gateway: logger cannot be nil")
		}
		config.logger = logger
		return nil
	})
}

// cloneStringMap creates an independent copy of a string map.
func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}

	cloned := make(map[string]string, len(source))
	maps.Copy(cloned, source)

	return cloned
}
