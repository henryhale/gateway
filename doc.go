// Package gateway provides a transport-independent, data-independent routing kernel.
//
// A Gateway selects an eligible provider, executes it, applies an explicit failure
// policy, and returns the provider's response. The package does not interpret,
// serialize, persist, or otherwise own application payloads.
//
// Gateway configuration is immutable after construction and Gateway is safe for concurrent use.
// Providers, filters, routing strategies, failure policies, and observers supplied
// by callers may be invoked concurrently and must therefore be concurrency-safe.
package gateway
