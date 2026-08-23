<div align=center>
<img src="./assets/logo.svg" width=45 height=45 />

# gateway

A generic orchestration framework for routing one standard request format across multiple external providers.

<a title="Go Reference" target="_blank" href="https://pkg.go.dev/github.com/henryhale/gateway"><img src="https://pkg.go.dev/badge/github.com/henryhale/gateway.svg"></a>
<a title="License" target="_blank" href="https://github.com/henryhale/gateway/blob/master/LICENSE.txt"><img src="https://img.shields.io/github/license/henryhale/gateway"></a>
<a title="GitHub Action" target="_blank" href="https://github.com/henryhale/gateway/actions/workflows/test.yml"><img src="https://github.com/henryhale/gateway/actions/workflows/test.yml/badge.svg"></a>
<a title="GitHub release" target="_blank" href="https://github.com/henryhale/gateway/releases"><img src="https://img.shields.io/github/release/henryhale/gateway.svg"></a>
<a title="Code Size" target="_blank" href="https://github.com/henryhale/gateway"><img src="https://img.shields.io/github/languages/code-size/henryhale/gateway.svg?style=flat-square"></a>

</div>


Use this package to build gateways for notifications, storage, search, or any system that needs:

- A unified application-facing interface.
- Bidirectional payload translation.
- Transport-independent custom providers.
- Built-in routing strategies.
- Retry and cross-provider fallback.
- Request timeouts and structured logging.

It is reasonable for:

  - A prototype or separate generic orchestration service.
  - Routing safe, idempotent reads among interchangeable upstreams.
  - Routing between redundant upstream endpoints inside one provider adapter.


## Installation

- Go 1.23 or later.
- No third-party runtime dependencies.

```bash
go get github.com/henryhale/gateway
```

## Example

A quotes gateway that routes one standard request across two quote APIs:

```go
quoteGateway, err := gw.New(
	gw.WithProviders(
		gw.UseProvider("zenquotes", zenQuotes, gw.WithOperations("quote.random")),
		gw.UseProvider("motivational-spark", motivationalSpark, gw.WithOperations("quote.random")),
	),
	gw.WithRouting(gw.RoundRobin()),
)
if err != nil {
	log.Fatal(err)
}
```

A runnable version of this gateway is in [_examples/quotes](_examples/quotes).

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

Released under MIT License. See [LICENSE.txt](./LICENSE.txt) for details.

&copy; 2026-present [Henry Hale](https://github.com/henryhale)
