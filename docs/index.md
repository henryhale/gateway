# Documentation

This guide covers everything needed to build a gateway with `gw`: defining
standard models, implementing providers, configuring routing and resilience,
handling errors, and integrating with an application framework.

## Contents

- [Developer workflow](#developer-workflow)
- [Standard application models](#standard-application-models)
- [HTTP provider codec](#http-provider-codec)
- [Transport-independent provider](#transport-independent-provider)
- [Gateway construction](#gateway-construction)
- [Built-in routing strategies](#built-in-routing-strategies)
- [Retry and fallback](#retry-and-fallback)
- [Error handling](#error-handling)
- [Framework integration](#framework-integration)
- [Logging](#logging)
- [Provider hints](#provider-hints)

## Developer workflow

A framework user performs five actions:

1. Define standard request and response structs.
2. Implement a provider codec or the transport-independent `Provider` interface.
3. Register providers.
4. Select a built-in routing strategy and resilience policies.
5. Call `HandleRequest` from any application framework.

## Standard application models

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

These are the only payment types used by application code. Provider-specific schemas remain inside provider adapters.

## HTTP provider codec

Implement `gw.Codec[Request, Response]` when the external provider uses HTTP.

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

Create the provider:

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

The HTTP adapter propagates `Request.ID` through `X-Request-ID` and `Request.IdempotencyKey` through `Idempotency-Key`. Responses are limited to 10 MiB by default; override `MaxResponseBytes` when necessary.

## Transport-independent provider

Implement `Provider` directly for an SDK, gRPC client, queue, local model, database, or any custom transport.

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

The complete contract is intentionally small:

```go
type Provider[RequestPayload any, ResponsePayload any] interface {
    Name() string
    Supports(operation Operation) bool
    Execute(
        ctx context.Context,
        request Request[RequestPayload],
    ) (ResponsePayload, error)
}
```

## Gateway construction

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

## Built-in routing strategies

### Priority

```go
gw.WithRouting(gw.Priority("fastpay", "safepay"))
```

Explicit names take precedence. Providers omitted from the list use their registered priority.

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

Uses explicit strategy weights first, then provider registration weights.

### Power of two choices

```go
gw.WithRouting(gw.PowerOfTwo(gw.ByObservedLatency()))
```

Samples two eligible providers and selects the lower-scored provider.

Available scorers:

```go
gw.ByObservedLatency()
gw.ByCost()
```

### Lowest cost

```go
gw.WithRouting(gw.LowestCost())
```

Uses values supplied through `gw.WithProviderCost`.

## Retry and fallback

Retries repeat a request against the same provider. Fallback selects a different provider.

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

Only errors marked `Retryable` are retried. `gw.HTTPProviderError` and the built-in HTTP transport mark transient HTTP and network failures appropriately.

Fallback is opt-in and only occurs for kinds selected by `FallbackOn`.

For operations that can create side effects, supply an idempotency key and define conservative retry/fallback policies.

## Error handling

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

Normalized error kinds:

- `validation`
- `authentication`
- `authorization`
- `rate_limit`
- `timeout`
- `unavailable`
- `rejected`
- `canceled`
- `internal`
- `unknown`

## Framework integration

`Gateway.HandleRequest` accepts `context.Context`, so it integrates directly with:

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

## Logging

`gw.WithLogger` accepts a standard library `*slog.Logger`, so gateway request lifecycle events (start, completion, fallback, and per-attempt failures) are emitted through whatever `slog.Handler` your application already uses.

```go
gateway, err := gw.New[Request, Response](
    // Providers and routing omitted.
    gw.WithLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil))),
)
```

If `WithLogger` is not supplied, the gateway logs through `slog.Default()`.

Because the logger is a plain `*slog.Logger`, no framework-specific interface needs to be implemented. Attach any `slog.Handler`, including adapters that forward to Zap, Zerolog, OpenTelemetry, or another observability backend, to route gateway logs into your existing pipeline.

## Provider hints

A request can target a registered provider directly:

```go
request.ProviderHint = "safepay"
```

The provider must support the operation. When fallback is enabled, another provider may be selected after the hinted provider fails with an allowed error kind.
