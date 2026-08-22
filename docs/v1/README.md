# Gateway v1 documentation

This guide covers building a gateway with v1: defining standard payloads,
implementing providers, registering them, configuring routing and failure
policy, and handling errors and telemetry.

The Go package name is `gateway` (`github.com/henryhale/gateway`). Every
example on this page imports it under the alias `gw`:

```go
import gw "github.com/henryhale/gateway"
```

v1 changes the API from v0.1.0. See [migration.md](migration.md) for the
before/after mapping; the previous release documentation remains under
[`docs/v0`](../v0/).

## Contents

- [Overview](#overview)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Core types](#core-types)
- [Providers](#providers)
- [HTTP providers](#http-providers)
- [Gateway construction](#gateway-construction)
- [Requests and results](#requests-and-results)
- [Routing strategies](#routing-strategies)
- [Filters](#filters)
- [Retry and failover](#retry-and-failover)
- [Cooldown and bulkheads](#cooldown-and-bulkheads)
- [Error handling](#error-handling)
- [Observability](#observability)
- [Provider hints](#provider-hints)
- [Examples](#examples)
- [Further reading](#further-reading)

## Overview

> Source: [`doc.go`](../../doc.go), [`gateway.go`](../../gateway.go)

A gateway routes one standard request to one of several interchangeable
providers. It selects an eligible provider, executes it, applies a failure
policy if the provider errors, and returns the provider's response.

The kernel treats application payloads as opaque. It does not serialize,
validate, or interpret them, and it holds no schema for them. Providers own
translation and domain semantics.

Building a gateway means doing five things:

1. Define standard request and response types per operation — see
   [Core types](#core-types).
2. Implement one [provider](#providers) per external service, either
   directly or through an [HTTP adapter](#http-providers).
3. Register the providers with [`gw.New`](#gateway-construction).
4. Choose a [routing strategy](#routing-strategies) and, if replay is safe,
   a [retry and failover](#retry-and-failover) policy.
5. Call `HandleRequest` from application code.

Two consequences of the opaque-payload design apply from the first line of
configuration:

- The kernel cannot infer replay safety from a provider error, so it never
  retries or fails over by default. Replay requires an explicit
  [`FailurePolicy`](#retry-and-failover).
- The kernel emits no logs, metrics, or traces. Telemetry is opt-in through
  an [`Observer`](#observability), and events carry no payloads.

Each attempt runs the same pipeline:

```text
Request
  -> providers already failed in this request are excluded
  -> static operation match
  -> cooldown check
  -> max in-flight check
  -> dynamic filters
  -> RoutingStrategy.Select
  -> Provider.Handle
  -> FailurePolicy.Decide  (only on error)
  -> Result
```

Configuration is immutable after `gw.New` returns, and a `*Gateway` is safe
for concurrent use. Providers, filters, routing strategies, failure
policies, and observers supplied by the caller may be invoked concurrently
and must therefore be safe for concurrent use themselves.

## Installation

- Go 1.23 or later.
- No third-party runtime dependencies.

```bash
go get github.com/henryhale/gateway
```

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    gw "github.com/henryhale/gateway"
)

func main() {
    echo := gw.ProviderFunc(func(_ context.Context, request gw.Request) (any, error) {
        return "handled: " + request.Value().(string), nil
    })

    gateway, err := gw.New(
        gw.WithProviders(
            gw.UseProvider("echo", echo, gw.WithOperations("echo")),
        ),
    )
    if err != nil {
        log.Fatal(err)
    }

    result, err := gateway.HandleRequest(
        context.Background(),
        gw.NewRequest("echo", "hello"),
    )
    if err != nil {
        log.Fatal(err)
    }

    text, ok := gw.ValueAs[string](result)
    if !ok {
        log.Fatal("unexpected result type")
    }
    fmt.Println(result.Provider(), text)
}
```

For a gateway that translates three live third-party APIs into one response
model, see [examples/quotes](../../examples/quotes/).

## Core types

> Source: [`request.go`](../../request.go), [`provider.go`](../../provider.go),
> [`routing.go`](../../routing.go), [`failure.go`](../../failure.go)

| Type | Purpose |
| --- | --- |
| `Operation` | String naming a capability, e.g. `"catalog.lookup"`. Eligibility is keyed on it. |
| `ProviderID` | Gateway-local provider identifier. Must be non-empty and unique. |
| `Request` | Immutable envelope: operation, opaque value, optional affinity key, request ID, and provider hint. |
| `Result` | A success: the opaque response value and the `ProviderID` that produced it. |
| `Provider` | The execution contract — one method. |
| `Filter` | Per-provider pre-call eligibility check. |
| `Candidate` | Read-only snapshot of one eligible provider, passed to routing. |
| `RoutingStrategy` | Selects one candidate. |
| `FailurePolicy` | Decides stop, retry, or next provider after a provider error. |
| `Observer` | Receives payload-free routing events. |
| `Error` | Gateway-level error carrying an `ErrorCode` and the underlying cause. |

Define the standard payloads once and reuse them across every provider:

```go
type ProductQuery struct {
    SKU    string
    Region string
}

type ProductResult struct {
    SKU       string
    Name      string
    Available bool
}
```

The gateway never reads these types. Provider-specific schemas stay inside
provider implementations and do not reach application code.

Every interface in the package has a function adapter, so small extensions
need no named type: `ProviderFunc`, `FilterFunc`, `RoutingFunc`,
`ScoreFunc`, `FailurePolicyFunc`, `ObserverFunc`, and `BackoffFunc`.

## Providers

> Source: [`provider.go`](../../provider.go)

A provider has one responsibility — execute a request:

```go
type Provider interface {
    Handle(context.Context, gw.Request) (any, error)
}
```

The execution target is unconstrained: an HTTP API, an SDK, a gRPC client,
a local function, a message queue, a database, or a model runtime.

### Multi-operation providers

One implementation can serve several operations by switching on the
operation and asserting the payload type:

```go
type CatalogService struct{ name string }

func (p CatalogService) Handle(ctx context.Context, request gw.Request) (any, error) {
    switch request.Operation() {
    case "catalog.lookup":
        return p.lookup(ctx, request.Value().(ProductQuery))
    case "catalog.availability":
        return p.availability(ctx, request.Value().(AvailabilityQuery))
    default:
        return nil, ErrUnsupported
    }
}
```

Capability is declared at registration rather than in the implementation:

```go
gw.UseProvider(
    "catalog-primary",
    CatalogService{name: "catalog-primary"},
    gw.WithOperations("catalog.lookup", "catalog.availability"),
)
```

Notes on operations:

- A provider registered **without** `WithOperations` is eligible for *every*
  operation. Declare operations explicitly unless a catch-all is intended.
- `gw.New` rejects empty operation strings. Duplicates within one
  registration are ignored.
- Operation-restricted providers are offered to routing before catch-all
  providers, but no strategy is required to prefer them.

### One implementation, many registrations

Because identity and routing metadata live at registration, the same
implementation can be registered repeatedly under different IDs, each with
its own credentials, region, priority, weight, cost, filters, and limits:

```go
gw.WithProviders(
    gw.UseProvider("catalog-eu", CatalogService{name: "catalog-eu"},
        gw.WithOperations("catalog.lookup"),
        gw.WithProviderPriority(1),
    ),
    gw.UseProvider("catalog-us", CatalogService{name: "catalog-us"},
        gw.WithOperations("catalog.lookup"),
        gw.WithProviderPriority(2),
    ),
)
```

### Concurrency

The gateway may call one provider from many goroutines at once. Providers
must be safe for concurrent use. Reuse concurrency-safe clients such as
`*http.Client`, and guard mutable provider-owned state with atomics,
mutexes, or channels.

## HTTP providers

> Source: [`httpgw/provider.go`](../../httpgw/provider.go),
> [`httpgw/forward.go`](../../httpgw/forward.go)

The root package stays transport-independent. `httpgw` is an optional
adapter layer for HTTP providers, in two flavours.

### Translated APIs

`httpgw.Provider` maps a standard request onto an HTTP call and back through
a `Codec`:

```go
type Codec interface {
    Encode(context.Context, gw.Request) (*http.Request, error)
    Decode(context.Context, gw.Request, *http.Response) (any, error)
}
```

```go
provider, err := httpgw.NewProvider(&http.Client{Timeout: 5 * time.Second}, codec)
```

Notes on `httpgw.Provider`:

- `Decode` receives the original request, so one codec can decode different
  response shapes per operation.
- `Provider` closes the response body after `Decode` returns. `Decode` must
  read everything it needs before returning.
- A nil client falls back to `http.DefaultClient`. A nil codec is an error.
- `Encode` returning a nil request is an error.

### Raw forwarding

`httpgw.ForwardProvider` forwards an `*http.Request` payload to one upstream
base URL and returns the `*http.Response` unbuffered:

```go
provider, err := httpgw.NewForwardProvider(http.DefaultClient, "https://upstream.example.com")
```

Notes on `httpgw.ForwardProvider`:

- The request payload must be an `*http.Request`; anything else is an error.
- `Handle` returns an `*http.Response` with **the body still open**. The
  caller owns it and must close it. This is what preserves streaming.
- The base URL must include a scheme and a host.

## Gateway construction

> Source: [`gateway.go`](../../gateway.go), [`options.go`](../../options.go)

`gw.New` takes functional options and returns a `*gw.Gateway`:

```go
gateway, err := gw.New(
    gw.WithProviders(
        gw.UseProvider(
            "catalog-primary",
            CatalogService{name: "catalog-primary"},
            gw.WithOperations("catalog.lookup", "catalog.availability"),
            gw.WithProviderPriority(1),
            gw.WithProviderCost(0.019),
            gw.WithMaxInFlight(100),
            gw.WithCooldown(gw.CooldownConfig{
                Failures: 3,
                Duration: 5 * time.Second,
                When:     func(err error) bool { return errors.Is(err, errUnavailable) },
            }),
        ),
        gw.UseProvider(
            "catalog-secondary",
            CatalogService{name: "catalog-secondary"},
            gw.WithOperations("catalog.lookup"),
            gw.WithProviderPriority(2),
            gw.WithProviderCost(0.021),
        ),
    ),
    gw.WithRouting(gw.Priority("catalog-primary", "catalog-secondary")),
    gw.WithFailurePolicy(gw.FailoverWhen(func(err error) bool {
        return errors.Is(err, errUnavailable)
    })),
    gw.WithRequestTimeout(10*time.Second),
    gw.WithMaxAttempts(4),
)
if err != nil {
    log.Fatal(err)
}
```

Gateway-level options:

| Option | Effect | Default |
| --- | --- | --- |
| `WithProviders(...)` | Registers providers. Repeatable; appends. | — (at least one required) |
| `WithRouting(s)` | Strategy that picks among eligible providers. | `gw.Priority()` |
| `WithFailurePolicy(p)` | Decides what happens after a provider error. | `gw.StopOnFailure()` |
| `WithRequestTimeout(d)` | Upper bound per `HandleRequest` call. Applied only when it would shorten the caller's existing deadline. `0` disables it. | `0` (disabled) |
| `WithMaxAttempts(n)` | Hard ceiling on `Provider.Handle` calls per request. | `len(providers) * 2`, minimum 1 |
| `WithObserver(o)` | Receives routing events. | none |

Per-provider options passed to `gw.UseProvider`:

| Option | Effect | Default |
| --- | --- | --- |
| `WithOperations(...)` | Restricts the provider to these operations. | every operation |
| `WithProviderPriority(n)` | Rank read by `Priority` routing. **Lower wins.** | `0` |
| `WithProviderWeight(n)` | Relative share read by `Weighted` routing. Must be greater than zero. | `1` |
| `WithProviderCost(c)` | Application-defined normalized cost read by `LowestCost` and `ByCost`. Must not be negative. | `0` |
| `WithMaxInFlight(n)` | Local concurrency ceiling. `0` means unlimited. | `0` |
| `WithCooldown(cfg)` | Suspends the provider after consecutive matching failures. | disabled |
| `WithFilter(f)` | Adds a pre-call eligibility check. Repeatable. | none |

`gw.New` returns an error — it never panics or silently drops
configuration — when: no provider is registered; a provider ID is empty or
duplicated; a provider is nil; an operation string is empty; a routing
strategy, failure policy, or observer is nil; the request timeout is
negative; or max attempts is not positive. Errors from per-provider options
surface from `gw.New`, not from `gw.UseProvider`.

## Requests and results

> Source: [`request.go`](../../request.go)

`gw.NewRequest` builds an immutable routing envelope around an opaque value:

```go
request := gw.NewRequest(
    "catalog.lookup",
    ProductQuery{SKU: "SKU-1001", Region: "eu"},
    gw.WithRequestID("req-8ac1"),
    gw.WithKey("catalog-eu"),
)
```

| Request option | Effect |
| --- | --- |
| `WithRequestID(id)` | Correlation ID echoed on every observability event. Not used for routing. |
| `WithKey(key)` | Affinity key read by `Sticky` and available to custom strategies. |
| `WithProviderHint(id)` | Requests a specific provider for the first attempt — see [Provider hints](#provider-hints). |

Accessors — `Operation()`, `Value()`, `Key()`, `ID()`, `ProviderHint()` —
are read-only. The gateway never modifies `Value`. If the value contains
mutable data, the caller and the provider must coordinate access to it.

A successful call returns a `Result` carrying the response and the provider
that produced it. `gw.ValueAs` recovers the concrete type:

```go
result, err := gateway.HandleRequest(ctx, request)
if err != nil {
    return err
}

product, ok := gw.ValueAs[ProductResult](result)
if !ok {
    return fmt.Errorf("unexpected result type from %s", result.Provider())
}
```

Check the second return value. A provider that returns a different type than
expected produces `ok == false` and a zero value, not a panic.

## Routing strategies

> Source: [`routing.go`](../../routing.go), `routing_*.go`

A strategy receives the eligible candidates and returns the index of the one
to call. It runs only after operation, cooldown, in-flight, and filter
checks, so every candidate it sees is already callable.

Each `Candidate` exposes:

| Accessor | Data |
| --- | --- |
| `ID()` | Provider ID. |
| `Priority()` | Configured priority. |
| `Weight()` | Configured weight. |
| `Cost()` | Configured cost. |
| `ObservedLatency()` | Exponentially weighted mean of observed call durations. |
| `InFlight()` | Calls active at selection time. |
| `FailureRate()` | Failures divided by total calls; `0` before the first call. |

Latency, in-flight, and failure-rate values are observed in the current
process only. They are not shared across instances.

### Priority

```go
gw.WithRouting(gw.Priority())
gw.WithRouting(gw.Priority("catalog-primary", "catalog-secondary"))
```

Without IDs, the candidate with the lowest `WithProviderPriority` value
wins; ties go to the first eligible candidate. With IDs, the earliest listed
ID that is eligible wins, and unlisted candidates are considered only when
no listed ID is eligible.

### Round robin

```go
gw.WithRouting(gw.RoundRobin())
```

Rotates across eligible candidates. The counter is gateway-wide, so all
operations share one rotation.

### Random

```go
gw.WithRouting(gw.Random())
```

Uniform selection using `crypto/rand`.

### Weighted

```go
gw.WithRouting(gw.Weighted())
```

Selects in proportion to each candidate's weight. Set shares with
`WithProviderWeight`.

### Lowest score

```go
gw.WithRouting(gw.Least(gw.ByCost()))
gw.WithRouting(gw.LowestLatency())  // Least(ByObservedLatency())
gw.WithRouting(gw.LeastBusy())      // Least(ByInFlight())
gw.WithRouting(gw.LowestCost())     // Least(ByCost())
```

`Least` picks the lowest-scoring candidate. Scorers are lower-is-better:
`ByObservedLatency`, `ByInFlight`, `ByCost`, and `ByFailureRate`. For a
custom score, implement `Scorer` (`Score(Request, Candidate) float64`) or
wrap a function in `gw.ScoreFunc`.

### Power of two choices

```go
gw.WithRouting(gw.PowerOfTwo(gw.ByInFlight()))
```

Samples two candidates at random and takes the lower-scoring one. Under
concurrency this spreads load that a strict minimum would concentrate on
whichever provider currently looks best to every goroutine at once.

### Sticky

```go
gw.WithRouting(gw.Sticky())

request := gw.NewRequest("chat", payload, gw.WithKey("conversation-abc"))
```

Rendezvous hashing over the request key and the eligible provider IDs. The
same key maps to the same provider for as long as that provider stays
eligible; when it drops out, only the keys mapped to it move.

A request with no key always selects the first eligible candidate. Set a key
on every request routed by `Sticky`.

### Custom strategies

```go
type LeastFailures struct{}

func (LeastFailures) Select(
    _ context.Context,
    _ gw.Request,
    candidates []gw.Candidate,
) (int, error) {
    best := 0
    for i := 1; i < len(candidates); i++ {
        if candidates[i].FailureRate() < candidates[best].FailureRate() {
            best = i
        }
    }
    return best, nil
}
```

`gw.RoutingFunc` adapts a plain function to the same interface.

Requirements for a custom strategy:

- `Select` must return an index into `candidates`. An out-of-range index
  fails the request with `routing_failed`. Returning an index rather than a
  provider keeps unregistered providers unreachable.
- `candidates` is never empty when `Select` is called.
- Implementations must be safe for concurrent use.
- Implementations must not retain `candidates` after `Select` returns; the
  gateway reuses the backing storage.

## Filters

> Source: [`provider.go`](../../provider.go), [`gateway.go`](../../gateway.go)

`WithOperations` covers static capability. A `Filter` covers everything that
changes at runtime:

```go
type Filter interface {
    Allow(context.Context, gw.Request, gw.Candidate) (bool, error)
}
```

```go
gw.UseProvider(
    "catalog-eu",
    catalogEU,
    gw.WithOperations("catalog.lookup"),
    gw.WithFilter(gw.FilterFunc(
        func(_ context.Context, request gw.Request, _ gw.Candidate) (bool, error) {
            query, ok := request.Value().(ProductQuery)
            return ok && query.Region == "eu", nil
        },
    )),
)
```

Filters can express region or language support, tenant allow and deny
rules, provider region, external quota or rate limits, distributed health
state, account-specific capacity, and request feature compatibility.
Cooldown state is process-local, so a filter backed by shared storage is
the way to coordinate health or quota across instances.

Notes on filters:

- Returning `false` removes only that provider from the candidate set.
- Returning an **error** fails the whole request with `routing_failed`. Use
  `false` for "not eligible" and reserve errors for a broken filter.
- Filters run on the request path for every attempt. They should not block.
- Filters must be safe for concurrent use.

## Retry and failover

> Source: [`failure.go`](../../failure.go), [`backoff.go`](../../backoff.go)

A `FailurePolicy` is consulted after each provider error and returns one of
three actions:

| Action | Effect |
| --- | --- |
| `Stop` | Fail the request immediately with `provider_failed`. |
| `RetryProvider` | Call the same provider again. |
| `NextProvider` | Exclude this provider for the rest of the request and route again. |

The default is `gw.StopOnFailure()`: no request is ever replayed unless the
application opts in.

Move to another provider on specific errors:

```go
gw.WithFailurePolicy(gw.FailoverWhen(func(err error) bool {
    return errors.Is(err, errUnavailable)
}))
```

Retry the same provider first, then fail over:

```go
gw.WithFailurePolicy(gw.RetryThenFailover(
    2,
    gw.ExponentialBackoff{
        Initial: 50 * time.Millisecond,
        Maximum: 500 * time.Millisecond,
    },
    func(err error) bool { return errors.Is(err, errTemporary) },
))
```

Notes on retry and failover:

- `maxRetries` counts retries *after* the first call to a provider, so `2`
  means up to three calls to it.
- The predicate decides replay safety. When it returns `false`, the policy
  stops. A nil predicate stops on every error.
- Once excluded by `NextProvider`, a provider cannot be selected again in
  that request. Retry and failover therefore cannot loop.
- Every request is bounded twice over: by `WithMaxAttempts` and by the
  request context. Exhausting the budget yields `attempts_exhausted`
  wrapping the last provider error.
- Backoff delays are interruptible. A context that ends during a delay fails
  the request with `canceled` or `deadline_exceeded`.

Available backoffs: `gw.NoBackoff()`, `gw.FixedBackoff(d)`, and
`gw.ExponentialBackoff{Initial, Maximum}`, which doubles from `Initial` and
clamps at `Maximum`. For anything else, including jitter, implement
`Backoff` (`Delay(attempt int) time.Duration`) or wrap a function in
`gw.BackoffFunc`.

For a fully custom decision, implement `FailurePolicy` directly or use
`gw.FailurePolicyFunc`. `Failure` carries the request, the provider, the
error, the total attempt number, and the per-provider attempt number;
return a `FailureDecision` holding a `FailureAction` and an optional
`Delay`.

### Non-idempotent operations

Do not enable replay merely because a client returned a timeout. A timeout
leaves the provider-side outcome unknown: an SMS may have been accepted while
the response was lost, and replaying it can send duplicate notifications.
Establish idempotency keys or reconciliation first, then narrow the predicate
to errors that are provably safe to replay.

## Cooldown and bulkheads

> Source: [`provider.go`](../../provider.go), [`gateway.go`](../../gateway.go)

Both limits are per-provider, opt-in, and process-local.

### Cooldown

Cooldown makes a provider ineligible for a fixed period after consecutive
matching failures:

```go
gw.WithCooldown(gw.CooldownConfig{
    Failures: 3,
    Duration: 5 * time.Second,
    When:     func(err error) bool { return errors.Is(err, errUnavailable) },
})
```

The `When` predicate decides which errors count. It is required in practice:
without it, a request rejection such as "invalid query" would be
treated as provider ill-health and take a working provider out of rotation.
`Failures` and `Duration` must both be greater than zero.

A successful call resets the counter. While a provider is in cooldown it is
skipped during candidate selection, and `Stats()` reports its
`CooldownUntil`.

### Bulkheads

`WithMaxInFlight` caps concurrent calls to one provider:

```go
gw.WithMaxInFlight(100)
```

The cap does not queue. A provider at its ceiling is simply not a candidate,
so another eligible provider is selected instead. If a provider is chosen and
then loses the race for the last slot, the gateway excludes it and reroutes
without spending an attempt from the budget.

## Error handling

> Source: [`errors.go`](../../errors.go)

Every failure from `HandleRequest` is a `*gw.Error`:

```go
type Error struct {
    Code      ErrorCode
    Operation Operation
    Provider  ProviderID
    Attempt   int
    Cause     error
}
```

| `ErrorCode` | Constant | Raised when |
| --- | --- | --- |
| `invalid_request` | `CodeInvalidRequest` | Nil gateway or context, empty operation, or a provider hint naming an unregistered provider. |
| `no_provider` | `CodeNoProvider` | No provider was eligible, and no provider had yet been tried. |
| `routing_failed` | `CodeRoutingFailed` | A filter returned an error, or the strategy returned an error or an out-of-range index. |
| `provider_failed` | `CodeProviderFailed` | A provider failed and the failure policy chose `Stop`. |
| `attempts_exhausted` | `CodeAttemptsExhausted` | The attempt budget ran out, or no provider remained eligible after at least one failure. |
| `deadline_exceeded` | `CodeDeadlineExceeded` | The request context deadline elapsed. |
| `canceled` | `CodeCanceled` | The request context was canceled. |

`Error.Unwrap` returns `Cause`, so `errors.Is` and `errors.As` reach the
provider's own error through the gateway error:

```go
result, err := gateway.HandleRequest(ctx, request)
if err != nil {
    if gwErr, ok := gw.AsError(err); ok {
        switch gwErr.Code {
        case gw.CodeNoProvider:
            return ErrNoCapacity
        case gw.CodeDeadlineExceeded, gw.CodeCanceled:
            return err
        default:
            log.Printf("gateway: %s provider=%s: %v",
                gwErr.Code, gwErr.Provider, gwErr.Cause)
        }
    }
    if errors.Is(err, errProductNotFound) {
        return ErrNotFound
    }
    return err
}
```

`Provider` is empty on errors raised before any provider was chosen.
`Attempt` is the number of provider calls made when the error was produced.

## Observability

> Source: [`observer.go`](../../observer.go), [`gateway.go`](../../gateway.go)

Nothing is logged, measured, or traced unless an observer is installed.
An observer is one method — `Observe(context.Context, Event)`:

```go
gw.WithObserver(gw.ObserverFunc(func(_ context.Context, event gw.Event) {
    if event.Type != gw.EventAttemptFinished {
        return
    }
    slog.Info("gateway attempt",
        "request", event.RequestID,
        "operation", event.Operation,
        "provider", event.Provider,
        "attempt", event.Attempt,
        "duration", event.Duration,
        "error", event.Error,
    )
}))
```

| Event | Emitted |
| --- | --- |
| `EventRequestStarted` | Before the first selection. |
| `EventProviderSelected` | After a provider is chosen and its concurrency slot acquired. |
| `EventAttemptFinished` | After each `Provider.Handle` call, with `Duration` and `Error`. |
| `EventRequestFinished` | After a success only. `Duration` covers the whole request. |

Notes on observers:

- `Event` carries no payloads — only event type, request ID, operation,
  provider ID, attempt number, duration, and error. SMS, catalog, and model
  payloads cannot leak through it.
- A failed request emits no `EventRequestFinished`; the error is returned to
  the caller instead.
- Observers run synchronously on the request path and should return quickly.
  Hand off to a buffered channel for anything expensive.
- A panic inside an observer is recovered and does not fail the request.
- Observers must be safe for concurrent use.

`Gateway.Stats` returns a `[]ProviderStats` — a point-in-time copy of the
same process-local counters that routing reads:

```go
for _, stats := range gateway.Stats() {
    fmt.Println(
        stats.Provider,
        stats.InFlight,
        stats.ObservedLatency,
        stats.Total,
        stats.Failures,
        stats.CooldownUntil,
    )
}
```

`CooldownUntil` is the zero `time.Time` when the provider is not in
cooldown.

## Provider hints

> Source: [`request.go`](../../request.go), [`gateway.go`](../../gateway.go)

A hint asks for one specific provider:

```go
request := gw.NewRequest(
    "catalog.lookup",
    query,
    gw.WithProviderHint("catalog-primary"),
)
```

Notes on hints:

- The hint applies to the **first** selection only. Any further attempt uses
  the routing strategy.
- The hinted provider must still pass the operation, cooldown, in-flight,
  and filter checks. If it does not, routing proceeds normally and no error
  is raised — a hint is a preference, not a pin.
- A hint naming an unregistered provider fails the request upfront with
  `invalid_request`.

## Examples

- `real` [Random quotes gateway](../../examples/quotes/README.md) —
  translates three live third-party HTTP APIs into one response model.
- `simulated` [HTTP reverse proxy](../../examples/http/main.go) — routes
  streaming HTTP requests across local upstream services.
- `simulated` [Catalog gateway](../../examples/catalog/main.go) —
  multi-operation providers, explicit failover, cooldown, and bulkheads.
- `simulated` [Weighted SMS routing](../../examples/sms/main.go) —
  distributes requests using provider weights.

## Further reading

- [Migrating from v0](migration.md) — before/after mapping for every changed
  API.
- [Benchmarks](benchmarks.md) — routing-kernel overhead and how to reproduce
  the numbers locally.
- [API reference on pkg.go.dev](https://pkg.go.dev/github.com/henryhale/gateway)

The v1 design follows [LiteLLM](https://github.com/BerriAI/litellm) as a
production gateway reference, adopting its architectural patterns rather
than its implementation: a deployment pool becomes provider registrations
plus an operation index, pre-call checks become `Filter`, deployment
cooldown becomes `CooldownConfig`, retries and fallbacks become
`FailurePolicy`, and callbacks become `Observer`. The differences are
deliberate: payloads stay opaque, replay is never inferred from a status
code, and no distributed cache is required.

- https://github.com/BerriAI/litellm
- https://github.com/BerriAI/litellm/blob/litellm_internal_staging/ARCHITECTURE.md
- https://github.com/BerriAI/litellm/blob/litellm_internal_staging/litellm/types/router.py
- https://github.com/BerriAI/litellm/blob/litellm_internal_staging/litellm/router_strategy/lowest_tpm_rpm.py
