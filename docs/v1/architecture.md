# Architecture

## Kernel boundary

The root package owns only orchestration:

```text
Request
  -> static operation index
  -> cooldown / bulkhead / dynamic filters
  -> RoutingStrategy
  -> Provider.Handle
  -> FailurePolicy
  -> Result
```

Payload serialization and provider-specific semantics are outside the kernel.

## Immutable static state

`New` compiles provider registrations into:

- an immutable provider slice;
- an immutable provider-ID index;
- an immutable operation-to-provider index;
- immutable provider filters and routing settings.

These structures are never changed after construction, so ordinary reads need no mutex.

## Mutable runtime state

Each provider has isolated atomic runtime counters:

- in-flight calls;
- EWMA latency;
- total calls;
- failures;
- consecutive cooldown-eligible failures;
- cooldown deadline.

No provider network call is executed while a gateway mutex is held.

## Request-local state

A request chain tracks excluded providers in a pooled boolean slice. After a provider is failed over, it cannot be selected again in that request. This prevents retry/fallback loops of the kind that can occur when retry and fallback state are not kept in one bounded execution state machine.

Candidate and exclusion buffers are pooled as pointer-backed structs. The minimal hot path is allocation-free after warm-up in the included benchmarks.

## Pre-call filters

Static operation capability is compiled into the operation index. Dynamic constraints use `Filter` and can model:

- country or currency support;
- tenant allow/deny rules;
- provider region;
- external quota or rate limits;
- distributed health state;
- account-specific capacity;
- request feature compatibility.

Filters are deliberately generic. A distributed deployment may consult Redis or another service in a custom filter without forcing every gateway user to depend on it.

## Routing

A strategy receives immutable `Candidate` snapshots and returns an index. Returning an index rather than a provider object prevents custom strategies from manufacturing or returning unregistered providers.

Built-ins avoid sorting on the request path:

- priority: O(n) minimum scan;
- round robin: O(1) selection after eligibility;
- random: O(1);
- weighted: O(n);
- least score: O(n);
- power of two: O(1) scoring after eligibility;
- sticky: O(n) rendezvous hashing.

## Reliability

Retry and failover are domain-sensitive. The default policy stops on the first provider error. A caller must explicitly choose replay behavior.

This avoids treating transport failures as universally retryable. For a payment authorization, a timeout can mean the provider committed the transaction but the response was lost. Replaying to the same or another provider could create a duplicate charge.

The execution state machine has a hard total attempt budget and context deadline. A failed provider is excluded before cross-provider failover.

## Cooldown

Cooldown is an opt-in provider setting. A caller supplies the predicate that decides which provider errors count toward cooldown. This prevents business rejections or validation failures from automatically marking an otherwise healthy provider unavailable.

## Bulkheads

`WithMaxInFlight` uses compare-and-swap to enforce a local concurrency ceiling. If a selected provider loses the capacity race, the request is rerouted without consuming a provider attempt.

## Observability

No logging, metrics, or tracing are enabled by default. `Observer` emits payload-free events only when configured. This keeps the kernel cheap and prevents accidental logging of payment, SMS, LLM, or HTTP payloads.

## HTTP

HTTP adapters live in `httpgw`, not the root package. `httpgw.Provider` supports bidirectional translated APIs. `httpgw.ForwardProvider` preserves streaming by returning the upstream `*http.Response` without buffering its body.
