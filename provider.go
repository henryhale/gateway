package gateway

import (
	"context"
	"errors"
	"time"
)

// Provider executes opaque gateway requests.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type Provider interface {
	Handle(context.Context, Request) (any, error)
}

// ProviderFunc adapts a function to the Provider interface.
type ProviderFunc func(context.Context, Request) (any, error)

// Handle executes the adapted provider function.
func (f ProviderFunc) Handle(ctx context.Context, request Request) (any, error) {
	return f(ctx, request)
}

// Filter performs a pre-call eligibility check for a provider.
//
// Implementations must be concurrency-safe and should avoid blocking whenever
// possible because filters run on the request path.
type Filter interface {
	Allow(context.Context, Request, Candidate) (bool, error)
}

// FilterFunc adapts a function to the Filter interface.
type FilterFunc func(context.Context, Request, Candidate) (bool, error)

// Allow evaluates the adapted eligibility function.
func (f FilterFunc) Allow(ctx context.Context, request Request, candidate Candidate) (bool, error) {
	return f(ctx, request, candidate)
}

// CooldownConfig configures local provider cooldown after repeated failures.
type CooldownConfig struct {
	Failures int
	Duration time.Duration
	When     func(error) bool
}

// ProviderRegistration describes one provider instance and its static routing data.
type ProviderRegistration struct {
	id          ProviderID
	provider    Provider
	operations  []Operation
	filters     []Filter
	priority    int
	weight      uint32
	cost        float64
	maxInFlight int64
	cooldown    CooldownConfig
	optionErr   error
}

// ProviderOption configures a ProviderRegistration.
type ProviderOption func(*ProviderRegistration) error

// UseProvider registers a provider under a stable gateway-local identifier.
func UseProvider(id ProviderID, provider Provider, options ...ProviderOption) ProviderRegistration {
	registration := ProviderRegistration{
		id:       id,
		provider: provider,
		weight:   1,
	}
	for _, option := range options {
		if option == nil || registration.optionErr != nil {
			continue
		}
		registration.optionErr = option(&registration)
	}
	return registration
}

// WithOperations limits a provider to the supplied operations.
func WithOperations(operations ...Operation) ProviderOption {
	return func(registration *ProviderRegistration) error {
		registration.operations = append([]Operation(nil), operations...)
		return nil
	}
}

// WithFilter adds a dynamic pre-call eligibility filter.
func WithFilter(filter Filter) ProviderOption {
	return func(registration *ProviderRegistration) error {
		if filter == nil {
			return errors.New("gateway: provider filter cannot be nil")
		}
		registration.filters = append(registration.filters, filter)
		return nil
	}
}

// WithProviderPriority sets the provider's static priority; lower values win.
func WithProviderPriority(priority int) ProviderOption {
	return func(registration *ProviderRegistration) error {
		registration.priority = priority
		return nil
	}
}

// WithProviderWeight sets the provider's relative weighted-routing share.
func WithProviderWeight(weight uint32) ProviderOption {
	return func(registration *ProviderRegistration) error {
		if weight == 0 {
			return errors.New("gateway: provider weight must be greater than zero")
		}
		registration.weight = weight
		return nil
	}
}

// WithProviderCost sets an application-defined normalized provider cost.
func WithProviderCost(cost float64) ProviderOption {
	return func(registration *ProviderRegistration) error {
		if cost < 0 {
			return errors.New("gateway: provider cost cannot be negative")
		}
		registration.cost = cost
		return nil
	}
}

// WithMaxInFlight sets a hard local concurrency limit for a provider.
func WithMaxInFlight(limit int64) ProviderOption {
	return func(registration *ProviderRegistration) error {
		if limit < 0 {
			return errors.New("gateway: max in-flight cannot be negative")
		}
		registration.maxInFlight = limit
		return nil
	}
}

// WithCooldown enables local cooldown after matching consecutive failures.
func WithCooldown(config CooldownConfig) ProviderOption {
	return func(registration *ProviderRegistration) error {
		if config.Failures <= 0 {
			return errors.New("gateway: cooldown failures must be greater than zero")
		}
		if config.Duration <= 0 {
			return errors.New("gateway: cooldown duration must be greater than zero")
		}
		registration.cooldown = config
		return nil
	}
}
