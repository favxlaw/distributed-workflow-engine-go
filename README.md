# distributed-workflow-engine-go

![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-blue)
![License MIT](https://img.shields.io/badge/License-MIT-green)

An order processing engine that enforces safe state transitions, prevents concurrent data corruption, and handles duplicate events — built to demonstrate production distributed systems patterns in Go.

## The Problem

In distributed systems, failures are not exceptional events—they are normal. Networks drop packets. Clients retry. Load balancers resubmit. The result: the same logical operation can arrive multiple times, concurrent requests race on the same data, and any service can set any state without enforcement.

Most systems tolerate this as an operational risk. This one solves it architecturally. The system cannot enter an invalid state, concurrent writes cannot corrupt data, and duplicate events cannot be applied twice. These are not features; they are enforced guarantees at the storage boundary.

## How It Works

### State Machine Enforcement

Orders move through a defined FSM and no transitions outside this path are permitted:

```
Created → Paid → Packed → Shipped → Delivered
   ↓        ↓                            
Cancelled (always possible until delivery)
```

All transitions are validated before any database call. A request to transition Packed → Delivered without going through Shipped is rejected immediately with a 422 error. Invalid transitions never reach the database.

### Optimistic Locking

Every order carries a version field. When a state transition is attempted, DynamoDB's `ConditionExpression` enforces that the version matches what the client read. If two requests race and one succeeds first, the second receives `ErrVersionConflict` and must retry with a fresh read. This prevents lost updates: the database cannot silently overwrite a concurrent modification.

### Idempotent Event Processing

Every transition request includes an `event_id`. Before processing, the system checks a `processed_events` table. If the event was already seen, the transition is skipped and a 200 response is returned. The table uses DynamoDB's conditional write so only the first concurrent request with the same event_id wins. TTL cleanup removes events after 24 hours. Safe to retry—duplicate events are silently skipped.

## Architecture

```
┌──────────────────────────────────────────┐
│           HTTP Clients                   │
└────────────────┬─────────────────────────┘
                 │
     ┌───────────┴────────────┐
     │                        │
     ▼                        ▼
POST /orders        POST /orders/{id}/transition
     │                        │
     └───────────┬────────────┘
                 │
     ┌───────────▼──────────────────┐
     │   HTTP API Layer             │
     │  - Validation                │
     │  - Error Mapping             │
     └───────────┬──────────────────┘
                 │
     ┌───────────▼──────────────────┐
     │   Storage & Events           │
     └───┬────────────────────┬──────┘
         │                    │
         ▼                    ▼
    ┌─────────────┐    ┌──────────────────┐
    │  DynamoDB   │    │  DynamoDB        │
    │  Orders     │    │  Events (TTL)    │
    │  (v-check)  │    │  (dedup)         │
    └─────────────┘    └──────────────────┘
```

The workflow layer (FSM) is imported by storage and validates before all writes. No state field is writable without passing through this enforcement.

## Project Structure

```
cmd/api/               Server entry point, config loading, graceful shutdown
internal/
  api/                 HTTP handlers, request validation, error mapping
  config/              Environment-based configuration (twelve-factor)
  events/              Idempotency layer, duplicate event detection
  storage/             DynamoDB persistence, optimistic locking
  workflow/            FSM definition, state types, transition rules
architecture/          Deep documentation and failure analysis
  diagrams/            System diagram, state machine visual, sequence diagrams
```

## Getting Started

### Prerequisites

- Go 1.22 or later
- Docker Compose
- AWS CLI (for local DynamoDB management)

### Run Locally

1. **Clone the repository:**
   ```bash
   git clone https://github.com/favxlaw/distributed-workflow-engine-go
   cd distributed-workflow-engine-go
   ```

2. **Copy environment file:**
   ```bash
   cp .env.example .env
   ```

3. **Start DynamoDB Local:**
   ```bash
   docker-compose up -d
   ```

4. **Create required tables:**
   ```bash
   # Orders table
   aws dynamodb create-table \
     --table-name orders \
     --attribute-definitions AttributeName=id,AttributeType=S \
     --key-schema AttributeName=id,KeyType=HASH \
     --billing-mode PAY_PER_REQUEST \
     --endpoint-url http://localhost:8000
   
   # Events table with TTL
   aws dynamodb create-table \
     --table-name processed_events \
     --attribute-definitions AttributeName=event_id,AttributeType=S \
     --key-schema AttributeName=event_id,KeyType=HASH \
     --billing-mode PAY_PER_REQUEST \
     --endpoint-url http://localhost:8000
   
   aws dynamodb update-time-to-live \
     --table-name processed_events \
     --time-to-live-specification AttributeName=expires_at,Enabled=true \
     --endpoint-url http://localhost:8000
   ```

5. **Start the server:**
   ```bash
   export $(cat .env | xargs) && go run cmd/api/main.go
   ```
   The server starts on port specified in `.env` (default: 8080).

### Run Tests

```bash
# Tests require DynamoDB Local running
go test ./internal/...

# With coverage
go test -cover ./internal/...
```

The test suite includes a concurrency simulation test (`internal/storage/concurrency_test.go`) that proves optimistic locking prevents data corruption under real concurrent load.

## API

### POST /orders

Create a new order in the Created state.

**Request:**
```json
{
  "id": "order-12345"
}
```

**Response (201 Created):**
```json
{
  "id": "order-12345",
  "state": "Created",
  "version": 0
}
```

**Error (409 Conflict if order exists):**
```json
{
  "error": "order already exists"
}
```

### POST /orders/{id}/transition

Transition an order to a new state. Requires an `event_id` to ensure idempotency.

**Request:**
```json
{
  "event_id": "evt-payment-001",
  "new_state": "Paid"
}
```

**Response (200 OK on success):**
```json
{
  "id": "order-12345",
  "state": "Paid",
  "version": 1
}
```

**Response (200 OK if duplicate event):**
```json
{
  "message": "event already processed"
}
```

**Error (409 Conflict on version conflict — retry with fresh read):**
```json
{
  "error": "version conflict, retry"
}
```

**Error (422 Unprocessable Entity on invalid transition):**
```json
{
  "error": "invalid state transition"
}
```

**Error (404 Not Found if order does not exist):**
```json
{
  "error": "order not found"
}
```

## Example Walkthrough

The following curl commands demonstrate the full lifecycle:

```bash
# Create an order (state = Created, version = 0)
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"id": "order-demo-1"}'

# Transition Created → Paid
curl -X POST http://localhost:8080/orders/order-demo-1/transition \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt-1", "new_state": "Paid"}'

# Transition Paid → Packed
curl -X POST http://localhost:8080/orders/order-demo-1/transition \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt-2", "new_state": "Packed"}'

# Try invalid transition: Packed → Delivered (skip Shipped) = 422
curl -X POST http://localhost:8080/orders/order-demo-1/transition \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt-3", "new_state": "Delivered"}'
# Returns 422: "invalid state transition"

# Send duplicate event with same event_id = 200 with message
curl -X POST http://localhost:8080/orders/order-demo-1/transition \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt-2", "new_state": "Packed"}'
# Returns 200: "event already processed"

# Create another order and transition to terminal state
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"id": "order-demo-2"}'
curl -X POST http://localhost:8080/orders/order-demo-2/transition \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt-4", "new_state": "Cancelled"}'

# Try transition from terminal state = 422
curl -X POST http://localhost:8080/orders/order-demo-2/transition \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt-5", "new_state": "Paid"}'
# Returns 422: "invalid state transition"
```

## Distributed Systems Concepts Demonstrated

- Finite State Machine enforcement at the storage boundary (prevents invalid state)
- Optimistic locking via DynamoDB ConditionExpression (prevents lost updates)
- Idempotent event processing with conditional writes (prevents duplicate processing)
- Typed domain errors mapped to HTTP semantics (correct status codes)
- Graceful shutdown with OS signal handling (no in-flight request loss)
- Environment-based configuration following twelve-factor principles
- Concurrency simulation test proving locking correctness under real load

## Documentation

Detailed architecture documentation is available in `/architecture`:

- [overview.md](architecture/overview.md) – System components, technology choices, scope boundaries
- [state-machine.md](architecture/state-machine.md) – FSM definition, terminal states, enforcement layers
- [idempotency.md](architecture/idempotency.md) – Event deduplication strategy, sequence correctness
- [concurrency.md](architecture/concurrency.md) – Optimistic locking, conflict handling, trade-offs
- [failure-modes.md](architecture/failure-modes.md) – Error scenarios, gap analysis, recovery patterns

Diagrams in `/architecture/diagrams`:

- [system-architecture.md](architecture/diagrams/system-architecture.md) – Component responsibility map
- [state-machine.md](architecture/diagrams/state-machine.md) – State transitions visual
- [concurrent-update-sequence.md](architecture/diagrams/concurrent-update-sequence.md) – Race condition timeline
- [event-flow.md](architecture/diagrams/event-flow.md) – Idempotency check flow

## Observability (Coming Soon)

Prometheus metrics and Grafana dashboards are planned for a future update. Metrics will cover version conflict rate, duplicate event rate, and transition latency — the numbers that matter for a distributed workflow engine. Watch this repo for updates.

## License

MIT
