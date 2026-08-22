# Migrating from v0 to v1

The earlier API bound one gateway instance to `Gateway[RequestPayload, ResponsePayload]`. The new API makes the routing kernel data-independent and moves typing to application/provider boundaries.

See the [v1 guide](README.md) for the full API. The previous release
documentation remains under [`docs/v0`](../v0/).

## Gateway construction

Before:

```go
gateway, err := gw.New[ForecastRequest, ForecastResponse](...)
```

Now:

```go
gateway, err := gw.New(...)
```

## Provider

Before:

```go
type Provider[Req any, Res any] interface {
    Name() string
    Supports(Operation) bool
    Execute(context.Context, Request[Req]) (Res, error)
}
```

Now:

```go
type Provider interface {
    Handle(context.Context, Request) (any, error)
}
```

Provider identity and static operation support move to registration:

```go
gw.UseProvider(
    "weather-service",
    weatherService,
    gw.WithOperations("weather.current", "weather.forecast"),
)
```

## Requests

Before:

```go
gw.Request[ForecastRequest]{
    Operation: "weather.forecast",
    Payload: forecastRequest,
}
```

Now:

```go
gw.NewRequest("weather.forecast", forecastRequest)
```

## Results

Before:

```go
response := result.Payload
```

Now:

```go
response, ok := gw.ValueAs[ForecastResponse](result)
```

## Retry and fallback

The previous API normalized several transport failures into retry/fallback flags. The new core defaults to `StopOnFailure()` and requires an explicit `FailurePolicy`. This is a deliberate safety change for non-idempotent operations.
