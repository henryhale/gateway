package testprovider

import (
	"context"
	"sync"
	"time"

	gw "github.com/henryhale/gateway"
)

// Outcome describes one scripted provider response.
type Outcome[ResponsePayload any] struct {
	Response ResponsePayload
	Error    error
	Delay    time.Duration
}

// Scripted is a deterministic transport-independent provider.
type Scripted[RequestPayload any, ResponsePayload any] struct {
	ProviderName string
	Operations   map[gw.Operation]bool
	Outcomes     []Outcome[ResponsePayload]

	mu    sync.Mutex
	calls int
}

// New creates a scripted provider with ordered outcomes.
func New[RequestPayload any, ResponsePayload any](
	name string,
	operations []gw.Operation,
	outcomes ...Outcome[ResponsePayload],
) *Scripted[RequestPayload, ResponsePayload] {
	supported := make(map[gw.Operation]bool, len(operations))
	for _, operation := range operations {
		supported[operation] = true
	}

	return &Scripted[RequestPayload, ResponsePayload]{
		ProviderName: name,
		Operations:   supported,
		Outcomes:     append([]Outcome[ResponsePayload](nil), outcomes...),
	}
}

// Name returns the scripted provider name.
func (p *Scripted[RequestPayload, ResponsePayload]) Name() string {
	return p.ProviderName
}

// Supports reports whether the scripted provider supports an operation.
func (p *Scripted[RequestPayload, ResponsePayload]) Supports(operation gw.Operation) bool {
	return p.Operations[operation]
}

// Execute returns the next configured provider outcome.
func (p *Scripted[RequestPayload, ResponsePayload]) Execute(
	ctx context.Context,
	_ gw.Request[RequestPayload],
) (ResponsePayload, error) {
	p.mu.Lock()
	index := p.calls
	p.calls++
	p.mu.Unlock()

	var zero ResponsePayload
	if len(p.Outcomes) == 0 {
		return zero, nil
	}
	if index >= len(p.Outcomes) {
		index = len(p.Outcomes) - 1
	}

	outcome := p.Outcomes[index]
	if outcome.Delay > 0 {
		timer := time.NewTimer(outcome.Delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-timer.C:
		}
	}

	return outcome.Response, outcome.Error
}

// Calls returns the number of Execute invocations.
func (p *Scripted[RequestPayload, ResponsePayload]) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
