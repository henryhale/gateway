# Routing

Routing happens only after static capability checks, cooldown checks, local bulkhead checks, and dynamic filters.

## Candidate data

Strategies can inspect:

- provider ID;
- priority;
- weight;
- normalized cost;
- observed EWMA latency;
- local in-flight count;
- local failure rate.

## Built-ins

```go
gateway.Priority()
gateway.Priority("primary", "secondary")
gateway.RoundRobin()
gateway.Random()
gateway.Weighted()
gateway.Least(gateway.ByCost())
gateway.LowestLatency()
gateway.LeastBusy()
gateway.PowerOfTwo(gateway.ByInFlight())
gateway.Sticky()
```

## Custom strategy

```go
type Strategy struct{}

func (Strategy) Select(
    ctx context.Context,
    request gateway.Request,
    candidates []gateway.Candidate,
) (int, error) {
    // Return an index into candidates.
    return 0, nil
}
```

A custom strategy must be concurrency-safe and must not retain the candidate slice after `Select` returns.

## Affinity

`Sticky()` uses rendezvous hashing. Add an affinity key to a request:

```go
request := gateway.NewRequest(
    "chat",
    payload,
    gateway.WithKey("conversation-abc"),
)
```

If a provider becomes ineligible, rendezvous hashing minimizes remapping among the remaining set.
