<div align=center>
<img src="./assets/logo.svg" width=45 height=45 />

# gateway

A generic orchestration and gateway framework for routing one standard request format across multiple external providers.

<a title="Go Reference" target="_blank" href="https://pkg.go.dev/github.com/henryhale/gateway"><img src="https://pkg.go.dev/badge/github.com/henryhale/gateway.svg"></a>
<a title="License" target="_blank" href="https://github.com/henryhale/gateway/blob/master/LICENSE.txt"><img src="https://img.shields.io/github/license/henryhale/gateway"></a>
<a title="GitHub Action" target="_blank" href="https://github.com/henryhale/gateway/actions/workflows/test.yml"><img src="https://github.com/henryhale/gateway/actions/workflows/test.yml/badge.svg"></a>
<a title="GitHub release" target="_blank" href="https://github.com/henryhale/gateway/releases"><img src="https://img.shields.io/github/release/henryhale/gateway.svg"></a>
<a title="Code Size" target="_blank" href="https://github.com/henryhale/gateway"><img src="https://img.shields.io/github/languages/code-size/henryhale/gateway.svg?style=flat-square"></a>

</div>


Use this package to build gateways for payments, LLMs, notifications, storage, search, or any system that needs:

- A unified application-facing interface.
- Bidirectional payload translation.
- Transport-independent custom providers.
- Built-in routing strategies.
- Retry and cross-provider fallback.
- Request timeouts and structured logging.

## Installation

- Go 1.23 or later.
- No third-party runtime dependencies.

```bash
go get github.com/henryhale/gateway
```

## Example

A simple payment gateway can be setup as shown below:

```go
package main

import (
    "log"
    
    gw "github.com/henryhale/gateway"
)

paymentGateway, err := gw.New(
    gw.WithProviders(
        gw.UseProvider(
            "fastpay",
            fastPay,
            gw.WithOperations("payment.charge"),
            gw.WithProviderPriority(1),
            gw.WithProviderWeight(70),
            gw.WithProviderCost(0.029),
        ),
        gw.UseProvider(
            "safepay",
            safePay,
            gw.WithOperations("payment.charge"),
            gw.WithProviderPriority(2),
            gw.WithProviderWeight(30),
            gw.WithProviderCost(0.032),
        ),
    ),
    gw.WithRouting(gw.PowerOfTwo(gw.ByObservedLatency())),
    gw.WithFailurePolicy(gw.StopOnFailure()),
    gw.WithRequestTimeout(10*time.Second),
)

if err != nil {
    log.Fatal(err)
}
```

The application calls one method:

```go
result, err := paymentGateway.HandleRequest(
    ctx,
    gw.NewRequest("payment.charge", charge),
)
if err != nil {
    log.Fatal(err)
}

response, ok := gw.ValueAs[ChargeResponse](result)
if !ok {
    log.Fatal("unexpected payment response")
}

log.Printf("provider=%s response=%+v", result.Provider(), response)
```

## Documentation

See [docs/README.md](docs/README.md) for the full guide.

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
