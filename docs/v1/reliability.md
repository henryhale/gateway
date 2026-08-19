# Reliability

## Safe default

`gateway.New` uses `StopOnFailure()`. No request is replayed unless the application explicitly opts in.

## Failover

```go
gateway.WithFailurePolicy(
    gateway.FailoverWhen(func(err error) bool {
        return errors.Is(err, ErrUnavailable)
    }),
)
```

## Retry then failover

```go
gateway.WithFailurePolicy(
    gateway.RetryThenFailover(
        2,
        gateway.ExponentialBackoff{
            Initial: 50 * time.Millisecond,
            Maximum: 500 * time.Millisecond,
        },
        func(err error) bool {
            return errors.Is(err, ErrTemporary)
        },
    ),
)
```

`maxRetries` means retries after the first provider call. The total request remains bounded by `WithMaxAttempts` and the request context.

## Payment safety

Do not configure automatic replay solely because an HTTP client returned a timeout. A timeout can leave the provider-side outcome unknown. Use provider idempotency keys or reconciliation semantics before declaring that error replay-safe.

## Cooldown

```go
gateway.WithCooldown(gateway.CooldownConfig{
    Failures: 3,
    Duration: 5 * time.Second,
    When: func(err error) bool {
        return errors.Is(err, ErrUnavailable)
    },
})
```

Cooldown state is process-local and lock-free. Distributed deployments can add a custom `Filter` backed by shared state when cross-instance coordination is required.

## Bulkheads

`WithMaxInFlight(n)` enforces a strict local per-provider concurrency ceiling. It does not queue requests; if a provider is full, another eligible provider can be selected.
