package gateway

// Operation identifies a logical action that providers may handle.
type Operation string

// ProviderID identifies one registered provider instance.
type ProviderID string

// Request is an immutable routing envelope around an opaque application value.
//
// Gateway never modifies Value. If Value contains mutable data, the caller and
// provider are responsible for coordinating access to that data.
type Request struct {
	operation    Operation
	value        any
	key          string
	id           string
	providerHint ProviderID
}

// RequestOption configures a Request created by NewRequest.
type RequestOption func(*Request)

// NewRequest creates a routing request for an operation and opaque value.
func NewRequest(operation Operation, value any, options ...RequestOption) Request {
	r := Request{operation: operation, value: value}
	for _, option := range options {
		if option != nil {
			option(&r)
		}
	}
	return r
}

// WithKey sets an optional affinity key used by routing strategies.
func WithKey(key string) RequestOption {
	return func(r *Request) { r.key = key }
}

// WithRequestID sets an optional request identifier for observability.
func WithRequestID(id string) RequestOption {
	return func(r *Request) { r.id = id }
}

// WithProviderHint requests a specific provider for the first attempt.
func WithProviderHint(id ProviderID) RequestOption {
	return func(r *Request) { r.providerHint = id }
}

// Operation returns the logical operation being routed.
func (r Request) Operation() Operation { return r.operation }

// Value returns the opaque application payload.
func (r Request) Value() any { return r.value }

// Key returns the optional affinity key.
func (r Request) Key() string { return r.key }

// ID returns the optional request identifier.
func (r Request) ID() string { return r.id }

// ProviderHint returns the optional first-choice provider.
func (r Request) ProviderHint() ProviderID { return r.providerHint }

// Result contains the successful provider and opaque application response.
type Result struct {
	provider ProviderID
	value    any
}

// Provider returns the provider that produced the successful result.
func (r Result) Provider() ProviderID { return r.provider }

// Value returns the opaque application response.
func (r Result) Value() any { return r.value }

// ValueAs extracts a typed value from a Result.
func ValueAs[T any](result Result) (T, bool) {
	value, ok := result.value.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return value, true
}
