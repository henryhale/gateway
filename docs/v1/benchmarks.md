# Benchmarks

Benchmark results from the build environment used for this archive:

```text
BenchmarkGatewaySingleProvider-5    ~331 ns/op    0 B/op    0 allocs/op
BenchmarkGatewayParallel-5          ~370 ns/op    0 B/op    0 allocs/op
```

Environment:

```text
go1.23.2 linux/amd64
AMD EPYC 9V74 80-Core Processor
```

These numbers measure routing-kernel overhead with in-process no-op providers; external provider latency is intentionally excluded. Results vary by CPU, Go release, strategy, filter count, and number of eligible providers.

Run locally:

```bash
go test -run '^$' -bench . -benchmem ./...
```
