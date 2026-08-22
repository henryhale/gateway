package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Gateway routes opaque requests across immutable provider registrations.
//
// Gateway is safe for concurrent use after construction.
type Gateway struct {
	providers      []providerEntry
	providerByID   map[ProviderID]int
	operationIndex map[Operation][]int
	wildcard       []int
	routing        RoutingStrategy
	failure        FailurePolicy
	timeout        time.Duration
	maxAttempts    int
	observer       Observer
	candidatePool  sync.Pool
	excludedPool   sync.Pool
}

type candidateBuffer struct {
	items []Candidate
}

type excludedBuffer struct {
	items []bool
}

type providerEntry struct {
	id          ProviderID
	provider    Provider
	operations  map[Operation]struct{}
	filters     []Filter
	priority    int
	weight      uint32
	cost        float64
	maxInFlight int64
	cooldown    CooldownConfig
	runtime     providerRuntime
}

type providerRuntime struct {
	inFlight              atomic.Int64
	latencyNanos          atomic.Int64
	total                 atomic.Uint64
	failures              atomic.Uint64
	consecutiveFailures   atomic.Int64
	cooldownUntilUnixNano atomic.Int64
}

// ProviderStats is a point-in-time snapshot of one provider's local runtime state.
type ProviderStats struct {
	Provider        ProviderID
	InFlight        int64
	ObservedLatency time.Duration
	Total           uint64
	Failures        uint64
	CooldownUntil   time.Time
}

// New constructs an immutable, concurrency-safe Gateway.
func New(options ...Option) (*Gateway, error) {
	cfg := config{
		routing: Priority(),
		failure: StopOnFailure(),
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}
	if len(cfg.providers) == 0 {
		return nil, errors.New("gateway: at least one provider is required")
	}

	g := &Gateway{
		providers:      make([]providerEntry, 0, len(cfg.providers)),
		providerByID:   make(map[ProviderID]int, len(cfg.providers)),
		operationIndex: make(map[Operation][]int),
		routing:        cfg.routing,
		failure:        cfg.failure,
		timeout:        cfg.timeout,
		maxAttempts:    cfg.maxAttempts,
		observer:       cfg.observer,
	}

	for _, registration := range cfg.providers {
		if registration.optionErr != nil {
			return nil, registration.optionErr
		}
		if registration.id == "" {
			return nil, errors.New("gateway: provider ID cannot be empty")
		}
		if registration.provider == nil {
			return nil, fmt.Errorf("gateway: provider %q cannot be nil", registration.id)
		}
		if _, exists := g.providerByID[registration.id]; exists {
			return nil, fmt.Errorf("gateway: duplicate provider ID %q", registration.id)
		}
		index := len(g.providers)
		g.providerByID[registration.id] = index
		var operationSet map[Operation]struct{}
		if len(registration.operations) > 0 {
			operationSet = make(map[Operation]struct{}, len(registration.operations))
		}
		g.providers = append(g.providers, providerEntry{
			id:          registration.id,
			provider:    registration.provider,
			operations:  operationSet,
			filters:     append([]Filter(nil), registration.filters...),
			priority:    registration.priority,
			weight:      registration.weight,
			cost:        registration.cost,
			maxInFlight: registration.maxInFlight,
			cooldown:    registration.cooldown,
		})

		if len(registration.operations) == 0 {
			g.wildcard = append(g.wildcard, index)
			continue
		}
		seen := make(map[Operation]struct{}, len(registration.operations))
		for _, operation := range registration.operations {
			if operation == "" {
				return nil, fmt.Errorf("gateway: provider %q has an empty operation", registration.id)
			}
			if _, exists := seen[operation]; exists {
				continue
			}
			seen[operation] = struct{}{}
			g.providers[index].operations[operation] = struct{}{}
			g.operationIndex[operation] = append(g.operationIndex[operation], index)
		}
	}

	if g.maxAttempts == 0 {
		g.maxAttempts = len(g.providers) * 2
		if g.maxAttempts < 1 {
			g.maxAttempts = 1
		}
	}

	providerCount := len(g.providers)
	g.candidatePool.New = func() any {
		return &candidateBuffer{items: make([]Candidate, 0, providerCount)}
	}
	g.excludedPool.New = func() any {
		return &excludedBuffer{items: make([]bool, providerCount)}
	}
	return g, nil
}

// HandleRequest selects providers, executes the configured failure policy, and returns the first success.
func (g *Gateway) HandleRequest(ctx context.Context, request Request) (Result, error) {
	if g == nil {
		return Result{}, &Error{
			Code:      CodeInvalidRequest,
			Operation: request.operation,
			Cause:     errors.New("nil gateway"),
		}
	}
	if ctx == nil {
		return Result{}, &Error{
			Code:      CodeInvalidRequest,
			Operation: request.operation,
			Cause:     errors.New("nil context"),
		}
	}
	if request.operation == "" {
		return Result{}, &Error{Code: CodeInvalidRequest, Cause: errors.New("operation is required")}
	}
	if request.providerHint != "" {
		if _, exists := g.providerByID[request.providerHint]; !exists {
			return Result{}, &Error{
				Code:      CodeInvalidRequest,
				Operation: request.operation,
				Cause:     fmt.Errorf("unknown provider hint %q", request.providerHint),
			}
		}
	}

	ctx, cancel := g.withTimeout(ctx)
	if cancel != nil {
		defer cancel()
	}
	started := time.Now()
	g.emit(ctx, Event{Type: EventRequestStarted, RequestID: request.id, Operation: request.operation})

	excludedBuffer := g.excludedPool.Get().(*excludedBuffer)
	excluded := excludedBuffer.items
	if len(excluded) != len(g.providers) {
		excluded = make([]bool, len(g.providers))
		excludedBuffer.items = excluded
	}
	defer func() {
		clear(excluded)
		g.excludedPool.Put(excludedBuffer)
	}()

	attempt := 0
	currentProvider := -1
	providerAttempt := 0
	firstSelection := true
	var lastErr error

	for attempt < g.maxAttempts {
		if err := ctx.Err(); err != nil {
			return Result{}, g.contextError(request.operation, currentProvider, attempt, err)
		}

		if currentProvider < 0 {
			selected, err := g.selectProvider(ctx, request, excluded, firstSelection)
			firstSelection = false
			if err != nil {
				if lastErr != nil {
					return Result{}, &Error{
						Code:      CodeAttemptsExhausted,
						Operation: request.operation,
						Attempt:   attempt,
						Cause:     lastErr,
					}
				}
				return Result{}, err
			}
			currentProvider = selected
			providerAttempt = 0
		}

		entry := &g.providers[currentProvider]
		if !entry.tryAcquire() {
			excluded[currentProvider] = true
			currentProvider = -1
			continue
		}

		attempt++
		providerAttempt++
		g.emit(
			ctx,
			Event{
				Type:      EventProviderSelected,
				RequestID: request.id,
				Operation: request.operation,
				Provider:  entry.id,
				Attempt:   attempt,
			},
		)
		attemptStarted := time.Now()
		value, providerErr := entry.provider.Handle(ctx, request)
		duration := time.Since(attemptStarted)
		entry.release()
		entry.observe(duration, providerErr)
		g.emit(
			ctx,
			Event{
				Type:      EventAttemptFinished,
				RequestID: request.id,
				Operation: request.operation,
				Provider:  entry.id,
				Attempt:   attempt,
				Duration:  duration,
				Error:     providerErr,
			},
		)

		if providerErr == nil {
			g.emit(
				ctx,
				Event{
					Type:      EventRequestFinished,
					RequestID: request.id,
					Operation: request.operation,
					Provider:  entry.id,
					Attempt:   attempt,
					Duration:  time.Since(started),
				},
			)
			return Result{provider: entry.id, value: value}, nil
		}
		lastErr = providerErr
		if contextErr := ctx.Err(); contextErr != nil {
			return Result{}, g.contextError(request.operation, currentProvider, attempt, contextErr)
		}

		decision := g.failure.Decide(ctx, Failure{
			Request:         request,
			Provider:        entry.id,
			Error:           providerErr,
			Attempt:         attempt,
			ProviderAttempt: providerAttempt,
		})
		switch decision.Action {
		case Stop:
			return Result{}, &Error{
				Code:      CodeProviderFailed,
				Operation: request.operation,
				Provider:  entry.id,
				Attempt:   attempt,
				Cause:     providerErr,
			}
		case RetryProvider:
			if err := wait(ctx, decision.Delay); err != nil {
				return Result{}, g.contextError(request.operation, currentProvider, attempt, err)
			}
		case NextProvider:
			excluded[currentProvider] = true
			currentProvider = -1
			providerAttempt = 0
			if err := wait(ctx, decision.Delay); err != nil {
				return Result{}, g.contextError(request.operation, -1, attempt, err)
			}
		default:
			return Result{}, &Error{
				Code:      CodeProviderFailed,
				Operation: request.operation,
				Provider:  entry.id,
				Attempt:   attempt,
				Cause:     providerErr,
			}
		}
	}

	return Result{}, &Error{
		Code:      CodeAttemptsExhausted,
		Operation: request.operation,
		Attempt:   attempt,
		Cause:     lastErr,
	}
}

// Stats returns a point-in-time copy of local provider runtime statistics.
func (g *Gateway) Stats() []ProviderStats {
	if g == nil {
		return nil
	}
	stats := make([]ProviderStats, len(g.providers))
	for i := range g.providers {
		entry := &g.providers[i]
		unixNano := entry.runtime.cooldownUntilUnixNano.Load()
		var cooldownUntil time.Time
		if unixNano > 0 {
			cooldownUntil = time.Unix(0, unixNano)
		}
		stats[i] = ProviderStats{
			Provider:        entry.id,
			InFlight:        entry.runtime.inFlight.Load(),
			ObservedLatency: time.Duration(entry.runtime.latencyNanos.Load()),
			Total:           entry.runtime.total.Load(),
			Failures:        entry.runtime.failures.Load(),
			CooldownUntil:   cooldownUntil,
		}
	}
	return stats
}

// selectProvider builds the current eligible set and asks the routing strategy for one index.
func (g *Gateway) selectProvider(
	ctx context.Context,
	request Request,
	excluded []bool,
	firstSelection bool,
) (int, error) {
	if firstSelection && request.providerHint != "" {
		index := g.providerByID[request.providerHint]
		candidate, allowed, err := g.candidate(ctx, request, index, excluded)
		if err != nil {
			return -1, err
		}
		if allowed {
			_ = candidate
			return index, nil
		}
	}

	candidateBuffer := g.candidatePool.Get().(*candidateBuffer)
	candidates := candidateBuffer.items[:0]
	defer func() {
		clear(candidates)
		candidateBuffer.items = candidates[:0]
		g.candidatePool.Put(candidateBuffer)
	}()

	for _, index := range g.operationIndex[request.operation] {
		candidate, allowed, err := g.candidate(ctx, request, index, excluded)
		if err != nil {
			return -1, err
		}
		if allowed {
			candidates = append(candidates, candidate)
		}
	}
	for _, index := range g.wildcard {
		candidate, allowed, err := g.candidate(ctx, request, index, excluded)
		if err != nil {
			return -1, err
		}
		if allowed {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return -1, &Error{
			Code:      CodeNoProvider,
			Operation: request.operation,
			Cause:     errors.New("no eligible provider"),
		}
	}

	selected, err := g.routing.Select(ctx, request, candidates)
	if err != nil {
		return -1, &Error{Code: CodeRoutingFailed, Operation: request.operation, Cause: err}
	}
	if selected < 0 || selected >= len(candidates) {
		return -1, &Error{
			Code:      CodeRoutingFailed,
			Operation: request.operation,
			Cause:     fmt.Errorf("routing strategy returned invalid candidate index %d", selected),
		}
	}
	return candidates[selected].index, nil
}

// candidate snapshots provider runtime state and evaluates dynamic filters.
func (g *Gateway) candidate(ctx context.Context, request Request, index int, excluded []bool) (Candidate, bool, error) {
	if index < 0 || index >= len(g.providers) || excluded[index] {
		return Candidate{}, false, nil
	}
	entry := &g.providers[index]
	if entry.operations != nil {
		if _, supported := entry.operations[request.operation]; !supported {
			return Candidate{}, false, nil
		}
	}
	now := time.Now().UnixNano()
	if until := entry.runtime.cooldownUntilUnixNano.Load(); until > now {
		return Candidate{}, false, nil
	}
	inFlight := entry.runtime.inFlight.Load()
	if entry.maxInFlight > 0 && inFlight >= entry.maxInFlight {
		return Candidate{}, false, nil
	}
	candidate := Candidate{
		index:           index,
		id:              entry.id,
		priority:        entry.priority,
		weight:          entry.weight,
		cost:            entry.cost,
		observedLatency: time.Duration(entry.runtime.latencyNanos.Load()),
		inFlight:        inFlight,
		total:           entry.runtime.total.Load(),
		failures:        entry.runtime.failures.Load(),
	}
	for _, filter := range entry.filters {
		allowed, err := filter.Allow(ctx, request, candidate)
		if err != nil {
			return Candidate{}, false, &Error{
				Code:      CodeRoutingFailed,
				Operation: request.operation,
				Provider:  entry.id,
				Cause:     fmt.Errorf("provider filter: %w", err),
			}
		}
		if !allowed {
			return Candidate{}, false, nil
		}
	}
	return candidate, true, nil
}

// tryAcquire atomically reserves local provider concurrency capacity.
func (e *providerEntry) tryAcquire() bool {
	if e.maxInFlight <= 0 {
		e.runtime.inFlight.Add(1)
		return true
	}
	for {
		current := e.runtime.inFlight.Load()
		if current >= e.maxInFlight {
			return false
		}
		if e.runtime.inFlight.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

// release returns one local provider concurrency slot.
func (e *providerEntry) release() { e.runtime.inFlight.Add(-1) }

// observe updates lock-free latency, counters, and optional cooldown state.
func (e *providerEntry) observe(duration time.Duration, err error) {
	e.runtime.total.Add(1)
	e.observeLatency(duration)
	if err == nil {
		e.runtime.consecutiveFailures.Store(0)
		return
	}
	e.runtime.failures.Add(1)
	if e.cooldown.Failures <= 0 || e.cooldown.Duration <= 0 {
		return
	}
	if e.cooldown.When != nil && !e.cooldown.When(err) {
		e.runtime.consecutiveFailures.Store(0)
		return
	}
	failures := e.runtime.consecutiveFailures.Add(1)
	if failures < int64(e.cooldown.Failures) {
		return
	}
	e.runtime.consecutiveFailures.Store(0)
	until := time.Now().Add(e.cooldown.Duration).UnixNano()
	for {
		current := e.runtime.cooldownUntilUnixNano.Load()
		if current >= until || e.runtime.cooldownUntilUnixNano.CompareAndSwap(current, until) {
			return
		}
	}
}

// observeLatency updates the provider's exponentially weighted latency estimate.
func (e *providerEntry) observeLatency(sample time.Duration) {
	sampleNanos := sample.Nanoseconds()
	for {
		current := e.runtime.latencyNanos.Load()
		next := sampleNanos
		if current > 0 {
			next = (current*7 + sampleNanos*3) / 10
		}
		if e.runtime.latencyNanos.CompareAndSwap(current, next) {
			return
		}
	}
}

// withTimeout applies the configured timeout only when it shortens the caller deadline.
func (g *Gateway) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if g.timeout <= 0 {
		return ctx, nil
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= g.timeout {
		return ctx, nil
	}
	return context.WithTimeout(ctx, g.timeout)
}

// contextError converts context termination into a gateway Error.
func (g *Gateway) contextError(operation Operation, providerIndex int, attempt int, err error) error {
	code := CodeCanceled
	if errors.Is(err, context.DeadlineExceeded) {
		code = CodeDeadlineExceeded
	}
	var provider ProviderID
	if providerIndex >= 0 && providerIndex < len(g.providers) {
		provider = g.providers[providerIndex].id
	}
	return &Error{Code: code, Operation: operation, Provider: provider, Attempt: attempt, Cause: err}
}

// emit sends an optional observability event without allowing observer panics to break routing.
func (g *Gateway) emit(ctx context.Context, event Event) {
	if g.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	g.observer.Observe(ctx, event)
}

// wait sleeps for a bounded failure-policy delay while respecting cancellation.
func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
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
