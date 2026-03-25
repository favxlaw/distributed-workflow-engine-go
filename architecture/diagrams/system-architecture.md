# System Architecture Diagram

```
                          ┌─────────────────────┐
                          │   HTTP Clients      │
                          │  (services, tests)  │
                          └──────────┬──────────┘
                                     │
                    POST /orders     │   POST /orders/{id}/transition
                                     │
                          ┌──────────▼──────────┐
                          │   HTTP API Layer    │
                          │  (net/http)         │
                          │                     │
                          │ - CreateOrder       │
                          │ - TransitionOrder   │
                          │ - Error Mapping     │
                          │ - JSON Response     │
                          └────┬────────┬──────┘
                               │        │
                    ┌──────────┘        └──────────┐
                    │                               │
                    ▼                               ▼
    ┌────────────────────────────┐  ┌──────────────────────────────┐
    │   Storage Layer            │  │   Events Layer               │
    │  (internal/storage)        │  │  (internal/events)           │
    │                            │  │                              │
    │ - GetOrder                 │  │ - IsProcessed                │
    │ - SaveOrder                │  │ - MarkProcessed              │
    │ - TransitionOrder          │  │ - Event deduplication        │
    │ - Optimistic locking       │  │ - TTL cleanup                │
    │ - Version checks           │  │                              │
    └────────────┬───────────────┘  └──────────────┬───────────────┘
                 │                                 │
                 ▼                                 ▼
    ┌────────────────────────────┐  ┌──────────────────────────────┐
    │   DynamoDB                 │  │   DynamoDB                   │
    │   (orders table)           │  │   (processed_events table)   │
    │                            │  │                              │
    │ PK: id                     │  │ PK: event_id                 │
    │ Attrs:                     │  │ Attrs:                       │
    │ - state                    │  │ - expires_at (TTL)           │
    │ - version                  │  │                              │
    │ - created_at               │  │ Conditional writes ensure    │
    │ - updated_at               │  │ only first request wins      │
    │                            │  │                              │
    │ Conditional writes         │  │                              │
    │ ensure only one            │  │                              │
    │ version update succeeds    │  │                              │
    └────────────────────────────┘  └──────────────────────────────┘


                          ┌─────────────────────┐
                          │  Workflow Layer     │
                          │ (internal/workflow) │
                          │                     │
                          │ - Order state       │
                          │ - FSM logic         │
                          │ - Transitions       │
                          │ - Validation        │
                          │                     │
                          │ (library, imported  │
                          │  by storage layer)  │
                          └─────────────────────┘


Legend:
→ Calls/reads from
↓ Writes to
```

## Layer Responsibilities

| Layer | Responsibility | Code Location |
|-------|---|---|
| **HTTP API** | Validate request shape, call correct layer, map errors to HTTP status codes, return JSON | `internal/api/` |
| **Storage** | Persist orders, enforce version checks, prevent concurrent updates | `internal/storage/` |
| **Events** | Track processed event IDs, prevent duplicate processing | `internal/events/` |
| **Workflow** | Define valid state transitions, validate before any write | `internal/workflow/` |

## Communication Style

- **Synchronous**: All calls between layers are synchronous. No queues, no async tasks.
- **No shared state**: Each layer is stateless. No caches, no connection pools.
- **Error propagation**: Errors flow up the stack. Each layer maps domain errors to appropriate status codes at the boundary.
