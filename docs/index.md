# Documentation

This guide covers everything needed to build a gateway: defining standard
models, implementing providers, configuring routing and resilience, handling
errors, and integrating with an application framework.

The Go package name is `gateway` (`github.com/henryhale/gateway`). Every
example on this page imports it under the alias `gw`:

```go
import gw "github.com/henryhale/gateway"
```

## Contents

- [Overview](#overview)
- [Core types](#core-types)
- [HTTP provider](#http-provider)
- [Transport-independent provider](#transport-independent-provider)
- [Gateway construction](#gateway-construction)
- [Routing strategies](#routing-strategies)
- [Retry and fallback](#retry-and-fallback)
- [Error handling](#error-handling)
- [Logging](#logging)
- [Framework integration](#framework-integration)
- [Provider hints](#provider-hints)

## Overview

Building a gateway means doing five things:

1. Define standard request and response structs for your operation.
2. Implement one [provider](#core-types) per external service, either as an
   [HTTP codec](#http-provider) or a [transport-independent provider](#transport-independent-provider).
3. Register providers with [`gw.New`](#gateway-construction).
4. Pick a [routing strategy](#routing-strategies) and, optionally, [retry and fallback](#retry-and-fallback) policies.
5. Call `HandleRequest` from any application framework — see [Framework integration](#framework-integration).

## Core types

> Source: [`types.go`](../types.go), [`provider.go`](../provider.go)

Every gateway is generic over one request payload and one response payload.
Application code only ever sees these standard types:

| Type | Purpose |
| --- | --- |
| `Operation` | A string identifying a capability, e.g. `"payment.charge"`. |
| `Request[T]` | The standard request: `Operation`, `Payload`, plus optional `ID`, `IdempotencyKey`, `ProviderHint`, and `Metadata`. |
| `Result[T]` | The standard response: `Payload`, the `Provider` that served it, and the `Attempts` made. |
| `Attempt` | One provider invocation: timing, `ErrorKind`, `ErrorCode`, and success. |
| `Usage` | Optional normalized resource consumption reported by a provider. |
| `ProviderState` | A provider candidate as seen by a routing strategy. |

Every provider — regardless of transport — implements the same small
interface:

```go
type Provider[RequestPayload any, ResponsePayload any] interface {
    Name() string
    Supports(operation Operation) bool
    Execute(ctx context.Context, request Request[RequestPayload]) (ResponsePayload, error)
}
```

Define your standard payloads once and reuse them across every provider:

```go
type ChargeRequest struct {
    AmountCents  int64  `json:"amount_cents"`
    Currency     string `json:"currency"`
    CustomerID   string `json:"customer_id"`
    PaymentToken string `json:"payment_token"`
}

type ChargeResponse struct {
    TransactionID string `json:"transaction_id"`
    Status        string `json:"status"`
}
```

Provider-specific schemas stay inside provider adapters and never leak into
application code.

## HTTP provider

> Source: [`http_provider.go`](../http_provider.go)

Use `NewHTTPProvider` when the external service speaks HTTP. It handles the
`net/http` request lifecycle for you; you only implement a `Codec` that
translates between your standard types and the provider's wire format.

```go
type Codec[RequestPayload any, ResponsePayload any] interface {
    Supports(operation Operation) bool
    Encode(ctx context.Context, request Request[RequestPayload]) (HTTPRequest, error)
    Decode(ctx context.Context, response HTTPResponse) (ResponsePayload, error)
}
```

```go
type FastPayCodec struct{}

// Supports reports whether this codec handles the operation.
func (FastPayCodec) Supports(operation gw.Operation) bool {
    return operation == "payment.charge"
}

// Encode translates the standard request into the provider request.
func (FastPayCodec) Encode(
    ctx context.Context,
    request gw.Request[ChargeRequest],
) (gw.HTTPRequest, error) {
    body, err := json.Marshal(map[string]any{
        "amount":   request.Payload.AmountCents,
        "currency": request.Payload.Currency,
        "customer": request.Payload.CustomerID,
        "token":    request.Payload.PaymentToken,
    })
    if err != nil {
        return gw.HTTPRequest{}, err
    }

    return gw.HTTPRequest{
        Method: http.MethodPost,
        Path:   "/v1/charges",
        Body:   body,
    }, nil
}

// Decode translates the provider response into the standard response.
func (FastPayCodec) Decode(
    ctx context.Context,
    response gw.HTTPResponse,
) (ChargeResponse, error) {
    if response.StatusCode < 200 || response.StatusCode >= 300 {
        return ChargeResponse{}, gw.HTTPProviderError(
            response.StatusCode,
            "fastpay_error",
            string(response.Body),
        )
    }

    var providerResponse struct {
        ID    string `json:"id"`
        State string `json:"state"`
    }
    if err := json.Unmarshal(response.Body, &providerResponse); err != nil {
        return ChargeResponse{}, err
    }

    return ChargeResponse{
        TransactionID: providerResponse.ID,
        Status:        providerResponse.State,
    }, nil
}
```

Create the provider from the codec:

```go
fastPay := gw.NewHTTPProvider(
    "fastpay",
    gw.HTTPProviderConfig{
        BaseURL: "https://api.fastpay.example",
        Headers: http.Header{
            "Authorization": []string{"Bearer " + os.Getenv("FASTPAY_API_KEY")},
        },
        Timeout:          5 * time.Second,
        MaxResponseBytes: 10 << 20,
    },
    FastPayCodec{},
)
```

Notes on `HTTPProviderConfig`:

- `Timeout` defaults to 30 seconds when unset (or when `Client` is also unset).
- `MaxResponseBytes` defaults to 10 MiB; oversized responses fail with `gw.CodeResponseTooLarge`.
- `Request.ID` is propagated through the `X-Request-ID` header, and `Request.IdempotencyKey` through `Idempotency-Key`.
- Supply your own `Client` (a `*http.Client`) to control transport, proxies, or TLS settings; `Timeout` is ignored when `Client` is set.

## Transport-independent provider

> Source: [`provider.go`](../provider.go)

Implement `Provider` directly for an SDK, gRPC client, queue, local model,
database, or any transport that isn't plain HTTP.

```go
type SDKProvider struct {
    client *vendor.Client
}

// Name returns the unique provider registration name.
func (p *SDKProvider) Name() string {
    return "vendor-sdk"
}

// Supports reports whether the provider handles an operation.
func (p *SDKProvider) Supports(operation gw.Operation) bool {
    return operation == "payment.charge"
}

// Execute performs translation and invokes the custom transport.
func (p *SDKProvider) Execute(
    ctx context.Context,
    request gw.Request[ChargeRequest],
) (ChargeResponse, error) {
    response, err := p.client.Charge(ctx, vendor.ChargeInput{
        Amount: request.Payload.AmountCents,
    })
    if err != nil {
        return ChargeResponse{}, normalizeVendorError(err)
    }

    return ChargeResponse{
        TransactionID: response.ID,
        Status:        response.Status,
    }, nil
}
```

Whatever error `Execute` returns is normalized on the way out — return a
`*gw.GatewayError` (see [Error handling](#error-handling)) when you can
classify the failure, or a plain `error` otherwise; the gateway wraps it with
`gw.ErrorInternal` and `gw.CodeAdapterError`.

## Gateway construction

> Source: [`gateway.go`](../gateway.go), [`options.go`](../options.go)

`gw.New` takes functional options and returns a `*Gateway[RequestPayload, ResponsePayload]`:

```go
paymentGateway, err := gw.New[ChargeRequest, ChargeResponse](
    gw.WithProviders(
        gw.UseProvider(
            fastPay,
            gw.WithProviderPriority(1),
            gw.WithProviderWeight(70),
            gw.WithProviderCost(0.029),
        ),
        gw.UseProvider(
            safePay,
            gw.WithProviderPriority(2),
            gw.WithProviderWeight(30),
            gw.WithProviderCost(0.032),
        ),
    ),
    gw.WithRouting(gw.PowerOfTwo(gw.ByObservedLatency())),
    gw.WithFallback(gw.FallbackOn(
        gw.ErrorRateLimited,
        gw.ErrorTimeout,
        gw.ErrorUnavailable,
    )),
    gw.WithRetry(gw.Retry{
        MaxAttempts: 2,
        Backoff: gw.ExponentialBackoff{
            Initial: 100 * time.Millisecond,
            Maximum: time.Second,
        },
    }),
    gw.WithRequestTimeout(10*time.Second),
    gw.WithLogger(slog.Default()),
)
if err != nil {
    log.Fatal(err)
}
```

At least one provider is required; `gw.New` returns an error otherwise, and
also rejects duplicate provider names and nil providers/options where
applicable.

`gw.UseProvider` accepts routing metadata for one provider:

| Option | Effect |
| --- | --- |
| `WithProviderPriority(n)` | Lower is preferred by `Priority` routing. Defaults to `100`. |
| `WithProviderWeight(n)` | Relative share used by `Weighted` routing. Defaults to `1`. |
| `WithProviderCost(c)` | Per-request cost used by `LowestCost` routing and `ByCost` scoring. |
| `WithProviderMetadata(m)` | Arbitrary metadata attached to the `ProviderState` seen by routing strategies. |

Options omitted from `gw.New` fall back to these defaults:

| Option | Default |
| --- | --- |
| `WithRouting` | `gw.Priority()` (registration order) |
| `WithRetry` | `gw.Retry{MaxAttempts: 1}` (no retries) |
| `WithFallback` | disabled (no cross-provider fallback) |
| `WithRequestTimeout` | `30 * time.Second` |
| `WithLogger` | `slog.Default()` |

## Routing strategies

> Source: [`routing.go`](../routing.go)

### Priority

```go
gw.WithRouting(gw.Priority("fastpay", "safepay"))
```

Explicit names take precedence, in the order listed. Providers omitted from
the list fall back to their registered `WithProviderPriority` value.

### Round robin

```go
gw.WithRouting(gw.RoundRobin())
```

Distributes calls across currently eligible providers in a stable rotation.

### Weighted

```go
gw.WithRouting(gw.Weighted(map[string]int{
    "fastpay": 70,
    "safepay": 30,
}))
```

Uses the weights passed here first, then falls back to each provider's
registered `WithProviderWeight`.

### Power of two choices

```go
gw.WithRouting(gw.PowerOfTwo(gw.ByObservedLatency()))
```

Samples two eligible providers at random and selects the one with the lower
score from the given `CandidateScorer`.

Built-in scorers:

```go
gw.ByObservedLatency() // prefers lower observed latency
gw.ByCost()            // prefers lower registered cost
```

### Lowest cost

```go
gw.WithRouting(gw.LowestCost())
```

Selects the provider with the lowest cost from `WithProviderCost`.

### Custom strategies

Implement `RoutingStrategy` directly to add a strategy not covered above:

```go
type RoutingStrategy interface {
    Name() string
    Select(ctx context.Context, candidates []ProviderState) (ProviderState, error)
}
```

## Retry and fallback

> Source: [`resilience.go`](../resilience.go)

Retries repeat a request against the **same** provider. Fallback moves to a
**different** provider. They are independent and can be combined.

```go
gw.WithRetry(gw.Retry{
    MaxAttempts: 2,
    Backoff: gw.ExponentialBackoff{
        Initial: 100 * time.Millisecond,
        Maximum: 2 * time.Second,
        Jitter:  0.20,
    },
})

gw.WithFallback(gw.FallbackOn(
    gw.ErrorRateLimited,
    gw.ErrorTimeout,
    gw.ErrorUnavailable,
))
```

- Only errors with `Retryable: true` are retried. `gw.HTTPProviderError` and
  the built-in HTTP transport already mark transient HTTP and network
  failures as retryable.
- Fallback is opt-in and only triggers for the error `Kind`s passed to
  `FallbackOn`, and only when the error is also marked `Fallbackable`.
- `ExponentialBackoff.Initial` and `.Maximum` default to `100ms` and `5s`
  respectively when left at zero; `Jitter` is a fraction (`0`–`1`) applied on
  top of the computed delay.

For operations with side effects, always set `Request.IdempotencyKey` and
choose retry/fallback policies conservatively.

## Error handling

> Source: [`errors.go`](../errors.go)

Every error returned by a gateway can be unwrapped into a `*gw.GatewayError`:

```go
result, err := paymentGateway.HandleRequest(ctx, request)
if err != nil {
    gatewayError, ok := gw.AsError(err)
    if !ok {
        return err
    }

    switch gatewayError.Kind {
    case gw.ErrorValidation:
        // Return 400.
    case gw.ErrorRateLimited:
        // Return 429 or queue the request.
    case gw.ErrorTimeout:
        // Return 504 or start reconciliation.
    default:
        // Return an application-safe gateway error.
    }
}
```

`GatewayError` carries two levels of detail:

- `Kind` (`gw.ErrorKind`) — a small, stable set of categories safe to branch
  on: `validation`, `authentication`, `authorization`, `rate_limit`,
  `timeout`, `unavailable`, `rejected`, `canceled`, `internal`, `unknown`.
- `Code` (`gw.ErrorCode`) — a finer-grained reason, useful for logging and
  metrics. Framework-generated codes (e.g. `gw.CodeResponseTooLarge`,
  `gw.CodeRoutingFailed`) are declared as constants in
  [`errors.go`](../errors.go); provider adapters may also set their own
  arbitrary `Code` values (as `FastPayCodec.Decode` does above with
  `"fastpay_error"`).

Construct normalized errors from an HTTP status with `gw.HTTPProviderError`,
which also classifies the `Kind` and sets `Retryable`/`Fallbackable` for
transient statuses (429, 5xx, and request/gateway timeouts).

## Logging

> Source: [`options.go`](../options.go) (`WithLogger`), [`gateway.go`](../gateway.go) (log call sites)

`gw.WithLogger` accepts a standard library `*slog.Logger`. The gateway logs
request lifecycle events — start, completion, fallback, and per-attempt
failures — through it, using whichever `slog.Handler` you configure:

```go
gateway, err := gw.New[Request, Response](
    // Providers and routing omitted.
    gw.WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil))),
)
```

If `WithLogger` is not supplied, the gateway logs through `slog.Default()`.

Because the logger is a plain `*slog.Logger`, no framework-specific interface
needs to be implemented. Attach any `slog.Handler` — including adapters that
forward to Zap, Zerolog, OpenTelemetry, or another backend — to route gateway
logs into your existing pipeline.

## Framework integration

`Gateway.HandleRequest` accepts a `context.Context`, so it integrates
directly with:

- `net/http`
- Gin, Echo, Fiber, Chi, and other HTTP routers
- gRPC and Connect handlers
- GraphQL resolvers
- Message consumers
- Scheduled workers
- CLI applications

Example `net/http` usage:

```go
func handler(gateway *gw.Gateway[ChargeRequest, ChargeResponse]) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var payload ChargeRequest
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            http.Error(w, "invalid JSON", http.StatusBadRequest)
            return
        }

        result, err := gateway.HandleRequest(r.Context(), gw.Request[ChargeRequest]{
            Operation:      "payment.charge",
            IdempotencyKey: r.Header.Get("Idempotency-Key"),
            Payload:        payload,
        })
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadGateway)
            return
        }

        w.Header().Set("X-Gateway-Provider", result.Provider)
        _ = json.NewEncoder(w).Encode(result.Payload)
    }
}
```

## Provider hints

> Source: [`types.go`](../types.go) (`Request.ProviderHint`), [`gateway.go`](../gateway.go) (`validateRequest`)

A request can target a registered provider directly, bypassing the routing
strategy for the first attempt:

```go
request.ProviderHint = "safepay"
```

The hinted provider must be registered and support the requested operation,
or `HandleRequest` returns a validation error before attempting anything.
When fallback is enabled, a different provider may still be selected after
the hinted provider fails with an allowed error kind.
