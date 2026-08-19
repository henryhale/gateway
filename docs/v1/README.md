# Gateway v1 documentation

Gateway v1 is a transport-independent, payload-opaque routing kernel for selecting and executing providers. It supports operation-based eligibility, configurable routing, explicit retry and failover policies, local cooldowns and bulkheads, and optional observability while leaving domain semantics and data translation to applications.

This version introduces breaking API changes from the published v0.1.0 release.Use this guide for the v1 design and API; the previous release documentation remains under [`docs/v0`](../v0/). See [migration guide - v0 to v1](../v0/v1.md).

## Table of contents

1. [Architecture](architecture.md) — kernel boundaries, state, request flow, concurrency, and adapters.
2. [Providers](providers.md) — provider contracts, multi-operation implementations, translation, and concurrency requirements.
3. [Routing](routing.md) — candidate data, built-in strategies, custom selection, and affinity.
4. [Reliability](reliability.md) — safe failure defaults, retry and failover policies, cooldowns, and bulkheads.
5. [LiteLLM patterns](litellm-patterns.md) — production gateway patterns adopted for v1 and intentional design differences.
6. [Benchmarks](benchmarks.md) — routing-kernel benchmark results and instructions for running them locally.
7. [API stability policy](api-stability.md) — the intended stable surface and compatibility rules for v1.

## Examples

- `real` [Random quotes gateway](../../examples/quotes/README.md) — translates three live third-party HTTP APIs into one response model.
- `simulated` [HTTP reverse proxy](../../examples/http/main.go) — routes streaming HTTP requests across local upstream services.
- `simulated` [Payment gateway](../../examples/payment/main.go) — demonstrates multi-operation providers, explicit failover, cooldowns, and bulkheads.
- `simulated` [Weighted SMS routing](../../examples/sms/main.go) — distributes SMS requests using provider weights.

## References

> This version is heavily inspired by [LiteLLM](https://github.com/BerriAI/litellm)'s architecture and gateway patterns it uses.

- https://github.com/BerriAI/litellm
- https://github.com/BerriAI/litellm/blob/litellm_internal_staging/ARCHITECTURE.md
- https://github.com/BerriAI/litellm/blob/litellm_internal_staging/litellm/types/router.py
- https://github.com/BerriAI/litellm/blob/litellm_internal_staging/litellm/router_strategy/lowest_tpm_rpm.py
