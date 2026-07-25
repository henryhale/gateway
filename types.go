package gateway

import "time"

// Operation identifies a provider capability such as payment.charge or llm.chat.
type Operation string

// Request contains the standard application payload submitted to a Gateway.
type Request[T any] struct {
	ID             string
	Operation      Operation
	IdempotencyKey string
	ProviderHint   string
	Payload        T
	Metadata       map[string]string
}

// Result contains the normalized response returned by a Gateway.
type Result[T any] struct {
	RequestID string
	Provider  string
	Payload   T
	Attempts  []Attempt
	Usage     Usage
}

// Attempt describes one provider invocation made while handling a request.
type Attempt struct {
	Provider  string
	Number    int
	StartedAt time.Time
	Duration  time.Duration
	ErrorKind ErrorKind
	ErrorCode string
	Success   bool
}

// Usage records optional normalized resource consumption reported by a provider.
type Usage struct {
	InputUnits  int64
	OutputUnits int64
	Cost        float64
	Currency    string
}

// ProviderState describes a provider candidate presented to a routing strategy.
type ProviderState struct {
	Name            string
	Priority        int
	Weight          int
	Cost            float64
	ObservedLatency time.Duration
	Healthy         bool
	Metadata        map[string]string
}
