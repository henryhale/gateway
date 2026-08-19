# Providers

A provider has one responsibility:

```go
type Provider interface {
    Handle(context.Context, gateway.Request) (any, error)
}
```

A provider may represent an HTTP API, SDK, gRPC client, local function, message queue, database, model runtime, or any other execution target.

## Multi-operation provider

```go
func (p *Provider) Handle(ctx context.Context, request gateway.Request) (any, error) {
    switch request.Operation() {
    case "payment.collect":
        return p.collect(ctx, request.Value().(Collection))
    case "payment.balance":
        return p.balance(ctx, request.Value().(BalanceRequest))
    default:
        return nil, ErrUnsupported
    }
}
```

Register capabilities separately from the provider implementation:

```go
gateway.UseProvider(
    "provider-a",
    provider,
    gateway.WithOperations("payment.collect", "payment.balance"),
)
```

A single provider implementation can therefore be registered multiple times with separate IDs, credentials, regions, priorities, weights, or filters.

## Translation

The provider owns standard-to-provider and provider-to-standard translation. For HTTP APIs, `httpgw.Codec` supplies an optional reusable adapter:

```go
type Codec interface {
    Encode(context.Context, gateway.Request) (*http.Request, error)
    Decode(context.Context, gateway.Request, *http.Response) (any, error)
}
```

Because `Decode` receives the original request, one codec can safely decode different operation response shapes.

## Concurrency

The gateway may call one provider concurrently from many goroutines. Reuse concurrency-safe clients such as `http.Client`; protect mutable provider-owned state with atomics, mutexes, channels, or another appropriate synchronization mechanism.
