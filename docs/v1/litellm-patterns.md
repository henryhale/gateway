# LiteLLM patterns adapted into gateway

This implementation studies LiteLLM as a production gateway reference and adopts architectural patterns rather than copying implementation code.

## Patterns retained

| LiteLLM concept | Generic gateway equivalent |
|---|---|
| model/deployment pool | provider registrations + operation index |
| routing strategies | `RoutingStrategy` |
| pre-call checks | `Filter` |
| deployment cooldown | `CooldownConfig` + atomic cooldown state |
| request exclusions during failover | request-local excluded-provider set |
| retries and fallbacks | `FailurePolicy` state machine |
| latency/usage signals | atomic provider runtime snapshots exposed in `Candidate` |
| session/deployment affinity | `Sticky()` rendezvous hashing |
| request limits | `WithMaxInFlight` local bulkhead; custom filters for external quotas |
| callbacks/telemetry | optional `Observer` |

## Intentional differences

### Opaque payloads

LiteLLM is deliberately LLM-aware. `gateway` does not know the payload schema. A provider owns translation and domain semantics.

### No implicit replay

The kernel does not infer retry or failover safety from HTTP status codes. Payments and other non-idempotent actions require domain-specific replay rules.

### One execution state machine

Retry and fallback are handled in one bounded loop with a total attempt budget and per-request provider exclusions. This avoids recursive retry/fallback cycles.

### No mandatory distributed cache

Distributed state is environment-dependent. Custom `Filter` implementations can query Redis or another shared service for quota/cooldown decisions, but single-process applications do not pay that cost.

### O(n) request-path strategy scans

The included strategies do not sort candidates per request. The gateway's static operation index removes providers that cannot support an operation before dynamic filters and routing run.

### No automatic logs

The kernel emits no logs unless an observer is configured. This reduces overhead and avoids accidental payload exposure.
