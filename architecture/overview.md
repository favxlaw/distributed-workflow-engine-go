# Architecture Overview

## The Problem

In distributed systems, things go wrong in familiar ways:

- **Invalid state transitions**: A payment API might attempt to mark an order as "Delivered" before it has been "Paid". Without enforcement, the database contains nonsense.
- **Concurrent updates**: Two services process the same order simultaneously. Both read version 0, both write, and one silent overwrites the other. You've lost an update and your data is inconsistent.
- **Duplicate events**: A request retries, a message broker redelivers, a service crashes and replays—and the same state change is applied twice. Your count is wrong or your workflow is corrupted.

Most systems treat these as operational edge cases that happen "rarely" and hope to catch them in production. This system treats them as fundamental architectural problems and solves them at the storage boundary.

## What This System Does

This is a minimal order processing service that models order lifecycle as a finite state machine and enforces correctness at every layer:

1. **Workflow Layer** – Defines allowed state transitions and validates them before any database writes
2. **Storage Layer** – Uses DynamoDB conditional writes to ensure concurrent updates are safe
3. **Events Layer** – Records processed event IDs to prevent duplicate state changes
4. **HTTP Layer** – Validates input shapes and maps domain errors to HTTP status codes

Each layer is independent, testable, and has clear responsibility boundaries. There is no shared state, no global configuration, and no magic.

## Why Go

Go was chosen for three reasons:

- **Concurrency primitives**: goroutines and channels make it trivial to model concurrent scenarios. The concurrency test uses exactly one `sync.WaitGroup` and channels—no locks, no condition variables.
- **Simplicity**: The standard library is production-grade. No external frameworks are needed. The entire HTTP layer is `net/http`. No configuration files, no dependency injection containers.
- **Strong typing**: A typo in a field name becomes a compile error, not a midnight alert. Errors are values that can be type-checked with `errors.As`, which forces callers to handle different failure modes explicitly.

## Why DynamoDB

DynamoDB was chosen for two reasons:

- **Conditional writes**: The `ConditionExpression` parameter is not an afterthought—it is the fundamental building block. Writing to a key only if a specific version matches is not a workaround; it is the primary use case. This directly solves the concurrency problem.
- **Managed scaling**: DynamoDB scales horizontally without connection pooling overhead. Go's goroutines model concurrent requests naturally, and DynamoDB handles burst traffic without shared connection limits.

The alternative—a relational database with advisory locks—would require more infrastructure and introduce contention points that don't exist here.

## Architecture Map

```
┌──────────────┐
│  HTTP Client │
└──────┬───────┘
       │
┌──────▼────────────────┐
│   HTTP API Layer      │ (internal/api)
│  - Validation         │
│  - Error mapping      │
└──────┬────────────────┘
       │
┌──────▼──────────────┐       ┌──────────────────┐
│  Storage Layer      │──────▶│   DynamoDB       │
│ (internal/storage)  │       │  Orders Table    │
│  - Optimistic lock  │       │  (version check) │
└──────┬──────────────┘       └──────────────────┘
       │
┌──────▼──────────────┐       ┌──────────────────┐
│  Events Layer       │──────▶│   DynamoDB       │
│ (internal/events)   │       │  Events Table    │
│  - Idempotency      │       │  (TTL cleanup)   │
└─────────────────────┘       └──────────────────┘

┌──────────────────┐
│  Workflow Layer  │ (internal/workflow)
│  - FSM           │ (used by storage & API)
│  - Transitions   │
└──────────────────┘
```

The workflow layer is not a separate service—it is a library imported by the storage layer. This ensures validation happens before any database operation.

## What This System Is NOT

- **No message broker**: Orders flow in via HTTP requests, not event streams. The events layer is for idempotency tracking, not event sourcing.
- **No saga orchestration**: This system does not manage distributed transactions across multiple services. It is a single service with a single source of truth (DynamoDB).
- **No authentication or authorization**: That responsibility belongs to the API gateway layer sitting in front of this service.
- **No retry logic**: When a client gets ErrVersionConflict or ErrDuplicateEvent, they are responsible for deciding whether and how to retry. This keeps the service simple and lets each client apply its own backoff policy.

Scope clarity matters. A system that tries to be everything is a system that fails audits for everything.
