package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// Gateway routes standard requests through registered providers.
type Gateway[RequestPayload any, ResponsePayload any] struct {
	providers      map[string]*providerEntry[RequestPayload, ResponsePayload]
	providerOrder  []string
	routing        RoutingStrategy
	fallback       FallbackPolicy
	retry          Retry
	requestTimeout time.Duration
	logger         *slog.Logger
}

type providerEntry[RequestPayload any, ResponsePayload any] struct {
	provider     Provider[RequestPayload, ResponsePayload]
	settings     providerSettings
	latencyNanos atomic.Int64
}

// New constructs a ready-to-use Gateway from providers and built-in policies.
func New[RequestPayload any, ResponsePayload any](
	options ...Option,
) (*Gateway[RequestPayload, ResponsePayload], error) {
	config := gatewayConfig{
		routing:        Priority(),
		retry:          Retry{MaxAttempts: 1},
		requestTimeout: 30 * time.Second,
		logger:         slog.Default(),
	}

	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option.apply(&config); err != nil {
			return nil, err
		}
	}

	gateway := &Gateway[RequestPayload, ResponsePayload]{
		providers:      make(map[string]*providerEntry[RequestPayload, ResponsePayload]),
		routing:        config.routing,
		fallback:       config.fallback,
		retry:          config.retry,
		requestTimeout: config.requestTimeout,
		logger:         config.logger,
	}

	for _, rawRegistration := range config.providers {
		registration, ok := rawRegistration.(ProviderRegistration[RequestPayload, ResponsePayload])
		if !ok {
			return nil, errors.New(
				"gateway: provider registration uses request or response types that do not match the gateway",
			)
		}

		if registration.provider == nil {
			return nil, errors.New("gateway: provider cannot be nil")
		}

		name := registration.provider.Name()
		if name == "" {
			return nil, errors.New("gateway: provider name cannot be empty")
		}
		if _, exists := gateway.providers[name]; exists {
			return nil, fmt.Errorf("gateway: duplicate provider name %q", name)
		}

		gateway.providers[name] = &providerEntry[RequestPayload, ResponsePayload]{
			provider: registration.provider,
			settings: registration.settings,
		}
		gateway.providerOrder = append(gateway.providerOrder, name)
	}

	if len(gateway.providers) == 0 {
		return nil, errors.New("gateway: at least one provider is required")
	}

	return gateway, nil
}

// HandleRequest routes one standard request and returns one standard response.
func (g *Gateway[RequestPayload, ResponsePayload]) HandleRequest(
	ctx context.Context,
	request Request[RequestPayload],
) (Result[ResponsePayload], error) {
	var zero Result[ResponsePayload]
	if g == nil {
		return zero, &GatewayError{
			Kind:    ErrorInternal,
			Code:    CodeNilGateway,
			Message: "gateway is nil",
		}
	}

	if err := g.validateRequest(request); err != nil {
		return zero, err
	}

	if request.ID == "" {
		request.ID = newRequestID()
	}

	requestContext, cancel := context.WithTimeout(ctx, g.requestTimeout)
	defer cancel()

	startedAt := time.Now()
	attempts := make([]Attempt, 0, len(g.providers))
	usedProviders := make(map[string]struct{}, len(g.providers))
	firstSelection := true

	g.logger.Log(
		requestContext,
		slog.LevelInfo,
		"gateway request started",
		"request_id", request.ID,
		"operation", request.Operation,
	)

	for {
		entry, err := g.selectProvider(
			requestContext,
			request,
			usedProviders,
			firstSelection,
		)
		firstSelection = false
		if err != nil {
			gatewayError := normalizeError(
				requestContext,
				err,
				"",
				request.Operation,
				len(attempts)+1,
			)
			return zero, gatewayError
		}

		usedProviders[entry.provider.Name()] = struct{}{}
		response, providerAttempts, providerError := g.executeProvider(
			requestContext,
			request,
			entry,
			len(attempts),
		)
		attempts = append(attempts, providerAttempts...)

		if providerError == nil {
			g.logger.Log(
				requestContext,
				slog.LevelInfo,
				"gateway request completed",
				"request_id", request.ID,
				"operation", request.Operation,
				"provider", entry.provider.Name(),
				"attempts", len(attempts),
				"duration", time.Since(startedAt),
			)
			return Result[ResponsePayload]{
				RequestID: request.ID,
				Provider:  entry.provider.Name(),
				Payload:   response,
				Attempts:  attempts,
			}, nil
		}

		if g.fallback == nil || !g.fallback.Allows(providerError) {
			g.logTerminalError(requestContext, request, providerError, len(attempts))
			return zero, providerError
		}

		if !g.hasEligibleProvider(request, usedProviders) {
			g.logTerminalError(requestContext, request, providerError, len(attempts))
			return zero, providerError
		}

		g.logger.Log(
			requestContext,
			slog.LevelWarn,
			"gateway fallback selected",
			"request_id", request.ID,
			"operation", request.Operation,
			"failed_provider", providerError.Provider,
			"error_kind", providerError.Kind,
		)
	}
}

// validateRequest verifies required request fields and optional provider hints.
func (g *Gateway[RequestPayload, ResponsePayload]) validateRequest(
	request Request[RequestPayload],
) error {
	if request.Operation == "" {
		return &GatewayError{
			Kind:    ErrorValidation,
			Code:    CodeOperationRequired,
			Message: "request operation is required",
		}
	}

	if request.ProviderHint != "" {
		entry, exists := g.providers[request.ProviderHint]
		if !exists {
			return &GatewayError{
				Kind:    ErrorValidation,
				Code:    CodeProviderHintUnknown,
				Message: fmt.Sprintf("provider hint %q is not registered", request.ProviderHint),
			}
		}
		if !entry.provider.Supports(request.Operation) {
			return &GatewayError{
				Kind: ErrorValidation,
				Code: CodeProviderHintUnsupported,
				Message: fmt.Sprintf(
					"provider %q does not support operation %q",
					request.ProviderHint,
					request.Operation,
				),
			}
		}
	}

	if !g.hasEligibleProvider(request, nil) {
		return &GatewayError{
			Kind:    ErrorValidation,
			Code:    CodeOperationUnsupported,
			Message: fmt.Sprintf("no provider supports operation %q", request.Operation),
		}
	}

	return nil
}

// selectProvider converts eligible registrations into routing candidates.
func (g *Gateway[RequestPayload, ResponsePayload]) selectProvider(
	ctx context.Context,
	request Request[RequestPayload],
	usedProviders map[string]struct{},
	firstSelection bool,
) (*providerEntry[RequestPayload, ResponsePayload], error) {
	if firstSelection && request.ProviderHint != "" {
		return g.providers[request.ProviderHint], nil
	}

	candidates := make([]ProviderState, 0, len(g.providers))
	for _, name := range g.providerOrder {
		if _, used := usedProviders[name]; used {
			continue
		}

		entry := g.providers[name]
		if !entry.provider.Supports(request.Operation) {
			continue
		}

		candidates = append(candidates, ProviderState{
			Name:            name,
			Priority:        entry.settings.priority,
			Weight:          entry.settings.weight,
			Cost:            entry.settings.cost,
			ObservedLatency: time.Duration(entry.latencyNanos.Load()),
			Healthy:         true,
			Metadata:        cloneStringMap(entry.settings.metadata),
		})
	}

	selected, err := g.routing.Select(ctx, candidates)
	if err != nil {
		return nil, &GatewayError{
			Kind:      ErrorUnavailable,
			Code:      CodeRoutingFailed,
			Message:   err.Error(),
			Operation: request.Operation,
			Cause:     err,
		}
	}

	entry, exists := g.providers[selected.Name]
	if !exists {
		return nil, &GatewayError{
			Kind:      ErrorInternal,
			Code:      CodeRoutingUnknownProvider,
			Message:   fmt.Sprintf("routing strategy selected unknown provider %q", selected.Name),
			Operation: request.Operation,
		}
	}
	_, used := usedProviders[selected.Name]
	if used || !entry.provider.Supports(request.Operation) {
		return nil, &GatewayError{
			Kind:      ErrorInternal,
			Code:      CodeRoutingIneligibleProvider,
			Message:   fmt.Sprintf("routing strategy selected ineligible provider %q", selected.Name),
			Operation: request.Operation,
		}
	}

	return entry, nil
}

// executeProvider invokes one provider with configured same-provider retries.
func (g *Gateway[RequestPayload, ResponsePayload]) executeProvider(
	ctx context.Context,
	request Request[RequestPayload],
	entry *providerEntry[RequestPayload, ResponsePayload],
	attemptOffset int,
) (ResponsePayload, []Attempt, *GatewayError) {
	var zero ResponsePayload
	maxAttempts := max(g.retry.MaxAttempts, 1)

	attempts := make([]Attempt, 0, maxAttempts)
	for localAttempt := 1; localAttempt <= maxAttempts; localAttempt++ {
		if err := ctx.Err(); err != nil {
			gatewayError := contextGatewayError(
				err,
				entry.provider.Name(),
				request.Operation,
				attemptOffset+localAttempt,
			)
			return zero, attempts, gatewayError
		}

		startedAt := time.Now()
		response, err := entry.provider.Execute(ctx, request)
		duration := time.Since(startedAt)
		entry.observeLatency(duration)

		attempt := Attempt{
			Provider:  entry.provider.Name(),
			Number:    attemptOffset + localAttempt,
			StartedAt: startedAt,
			Duration:  duration,
			Success:   err == nil,
		}

		if err == nil {
			attempts = append(attempts, attempt)
			return response, attempts, nil
		}

		gatewayError := normalizeError(
			ctx,
			err,
			entry.provider.Name(),
			request.Operation,
			attemptOffset+localAttempt,
		)
		attempt.ErrorKind = gatewayError.Kind
		attempt.ErrorCode = gatewayError.Code
		attempts = append(attempts, attempt)

		g.logger.Log(
			ctx,
			slog.LevelWarn,
			"gateway provider attempt failed",
			"request_id", request.ID,
			"operation", request.Operation,
			"provider", entry.provider.Name(),
			"attempt", attempt.Number,
			"error_kind", gatewayError.Kind,
			"error_code", gatewayError.Code,
		)

		if !gatewayError.Retryable || localAttempt == maxAttempts {
			return zero, attempts, gatewayError
		}

		if err := waitForBackoff(ctx, g.retry.Backoff, localAttempt); err != nil {
			return zero, attempts, contextGatewayError(
				err,
				entry.provider.Name(),
				request.Operation,
				attemptOffset+localAttempt,
			)
		}
	}

	return zero, attempts, &GatewayError{
		Kind:      ErrorInternal,
		Code:      CodeRetryLoopExhausted,
		Message:   "provider retry loop ended unexpectedly",
		Provider:  entry.provider.Name(),
		Operation: request.Operation,
	}
}

// observeLatency updates the provider's exponentially weighted latency estimate.
func (e *providerEntry[RequestPayload, ResponsePayload]) observeLatency(sample time.Duration) {
	for {
		current := e.latencyNanos.Load()
		next := sample.Nanoseconds()
		if current > 0 {
			next = (current*7 + sample.Nanoseconds()*3) / 10
		}
		if e.latencyNanos.CompareAndSwap(current, next) {
			return
		}
	}
}

// hasEligibleProvider reports whether an unused provider supports the operation.
func (g *Gateway[RequestPayload, ResponsePayload]) hasEligibleProvider(
	request Request[RequestPayload],
	usedProviders map[string]struct{},
) bool {
	for name, entry := range g.providers {
		if usedProviders != nil {
			if _, used := usedProviders[name]; used {
				continue
			}
		}
		if entry.provider.Supports(request.Operation) {
			return true
		}
	}
	return false
}

// logTerminalError records a terminal request failure.
func (g *Gateway[RequestPayload, ResponsePayload]) logTerminalError(
	ctx context.Context,
	request Request[RequestPayload],
	err *GatewayError,
	attempts int,
) {
	g.logger.Log(
		ctx,
		slog.LevelError,
		"gateway request failed",
		"request_id", request.ID,
		"operation", request.Operation,
		"provider", err.Provider,
		"attempts", attempts,
		"error_kind", err.Kind,
		"error_code", err.Code,
	)
}

// contextGatewayError converts context termination into a normalized error.
func contextGatewayError(
	err error,
	provider string,
	operation Operation,
	attempt int,
) *GatewayError {
	kind := ErrorCanceled
	code := CodeRequestCanceled
	message := "request was canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		kind = ErrorTimeout
		code = CodeRequestTimeout
		message = "gateway request timed out"
	}

	return &GatewayError{
		Kind:      kind,
		Code:      code,
		Message:   message,
		Provider:  provider,
		Operation: operation,
		Attempt:   attempt,
		Cause:     err,
	}
}

// newRequestID creates a compact random request identifier.
func newRequestID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}

	return fmt.Sprintf("gateway-%d", time.Now().UnixNano())
}
