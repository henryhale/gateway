# API stability policy

The intended long-lived kernel surface is deliberately small:

```go
type Request
type Result
type Provider
type Filter
type Candidate
type RoutingStrategy
type Failure
type FailurePolicy
type Gateway

func New(...Option) (*Gateway, error)
func (*Gateway) HandleRequest(context.Context, Request) (Result, error)
```

## Stability rules

1. The gateway kernel remains transport-independent and payload-opaque.
2. Existing exported interfaces are not expanded with new required methods after a stable v1 tag.
3. New behavior is added through new optional interfaces, options, adapters, or helper implementations.
4. `Provider.Handle`, `RoutingStrategy.Select`, `Filter.Allow`, and `FailurePolicy.Decide` remain the core extension contracts.
5. The default failure policy remains non-replaying unless a future major version explicitly changes that behavior.
6. A constructed gateway remains safe for concurrent use.
7. Root-package runtime dependencies remain limited to the Go standard library unless a future major version explicitly changes the policy.

## Why interfaces are intentionally narrow

Adding methods to a Go interface breaks every third-party implementation. The framework therefore keeps each extension point focused on one responsibility:

- Provider: execute;
- Filter: eligibility;
- RoutingStrategy: selection;
- FailurePolicy: retry/failover decision;
- Observer: telemetry.

Optional capabilities should be introduced as separate interfaces instead of expanding these contracts.
