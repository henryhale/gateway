<div align=center>
<img src="./assets/logo.svg" width=45 height=45 />

# gateway

A generic orchestration and gateway framework for routing one standard request format across multiple external providers.

</div>


Use this package to build gateways for payments, LLMs, notifications, storage, search, or any system that needs:

- A unified application-facing interface.
- Bidirectional payload translation.
- Transport-independent custom providers.
- Built-in routing strategies.
- Retry and cross-provider fallback.
- Request timeouts and structured logging.

A simple payment gateway can be setup as shown below:

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

The application calls one method:

```go
result, err := paymentGateway.HandleRequest(ctx, gw.Request[ChargeRequest]{
    Operation: "payment.charge",
    Payload:   charge,
})
```

## Installation

- Go 1.23 or later.
- No third-party runtime dependencies.

```bash
go get github.com/henryhale/gateway
```

## Documentation

See [docs/index.md](docs/index.md) for the full guide.

## Development

Run all release checks:

```bash
make check
```

Individual commands:

```bash
go fmt ./...
go vet ./...
go test -race ./...
```

## License

MIT. See [LICENSE](LICENSE.txt) for details.
