# Idempotent Event Processing

## What Idempotency Means

An operation is idempotent if performing it multiple times has the same effect as performing it once.

Example: 
- "Set my homepage to google.com" is idempotent. Setting it five times is the same as setting it once.
- "Increment my balance by $100" is NOT idempotent. Doing it five times gives you $500 extra.

Idempotency is critical in distributed systems because retries are universal.

## Why Distributed Systems Cannot Assume At-Most-Once Delivery

The internet is unreliable:

1. **Network timeouts**: A request times out after 30 seconds. The client retries. But the first request succeeded—the database already recorded the state change. The second request should not apply the same change again.

2. **Client-side retries**: A mobile app sends a "transition order to Paid" request. The response gets lost before reaching the client. The client's retry logic resends. Same event, twice in rapid succession.

3. **Load balancer retries**: An AWS Network Load Balancer considers the backend unhealthy and retries the request to a different instance. The unhealthy instance might have already processed it.

4. **Message broker redelivery**: If this service consumed events from a broker, that broker might redeliver the same message on failure.

In all these cases, the same logical event arrives multiple times. Without idempotency protection, the state change is applied multiple times.

## The Processed Events Table

This system uses a simple approach:

### Table Schema

```
TableName: processed_events
PartitionKey: event_id (String)
Attributes:
  - event_id: String (PK)
  - expires_at: Number (TTL expiration timestamp)
```

The `expires_at` attribute is a DynamoDB TTL field. Items older than 24 hours are automatically deleted by DynamoDB.

### How It Works

When a request arrives with an `event_id`:

1. **Check if already processed**: Query for the event_id. If it exists, this request is a duplicate.
2. **Mark as processed**: On first arrival, write the event_id with a TTL timestamp. Use a conditional write so only the first concurrent request succeeds.

### Key Insight: Conditional Writes

When `MarkProcessed` is called, it uses:

```go
ConditionExpression: "attribute_not_exists(event_id)"
```

This means: "Write only if the event_id does not already exist."

If two requests with the same event_id arrive simultaneously:

- Request A writes first and succeeds
- Request B attempts to write and gets `ConditionalCheckFailedException`
- The exception is caught and converted to `ErrDuplicateEvent`

Both requests are aware they are processing the same event. One proceeds, one is aware it is a duplicate.

## The Transition Sequence

In `internal/api/handler.go`, the `TransitionOrder` method follows this exact sequence:

```go
1. Check IsProcessed(event_id)
   - If true: return 200 "event already processed", exit
   
2. Fetch order with GetOrder(id)
   - If not found: return 404, exit
   
3. Transition state with TransitionOrder(order, newState) 
   - Updates version, validates transition
   - Version conflict possible here
   
4. Mark as processed with MarkProcessed(event_id)
   - If this fails with ErrDuplicateEvent: return 200, exit
   - (Another concurrent request won the race to mark it)
   
5. Return 200 with new order state
```

## Why Sequence Order Matters

The natural instinct is to mark the event as processed *first*, then transition the order:

```go
// WRONG:
MarkProcessed(eventID)
TransitionOrder(order, newState)
```

This is catastrophic. If `TransitionOrder` fails—say, due to a version conflict—the event is already marked as processed. A retry will see the event as "already processed" and skip the transition entirely. The order never transitions. Silent data loss.

The correct sequence marks the event *after* the transition:

```go
// CORRECT:
TransitionOrder(order, newState)    // Must succeed first
MarkProcessed(eventID)              // Mark after success
```

On retry:
- `IsProcessed` returns true, request exits with 200
- *But* the order has been transitioned

If `MarkProcessed` fails but `TransitionOrder` succeeded, the next retry will see `IsProcessed == false` and attempt `TransitionOrder` again. The second transition will fail with `ErrVersionConflict` (because version advanced), and the caller will receive a 409 error, prompting another retry.

This is a gap in the current design—see failure-modes.md for discussion.

## The Race Condition: Two Concurrent Requests

Scenario: Two requests with the same `event_id` arrive simultaneously for the same order.

```
Request A: IsProcessed(evt-1) -> false
Request B: IsProcessed(evt-1) -> false

Request A: TransitionOrder(Create->Paid) succeeds, version 0->1
Request B: TransitionOrder(Create->Paid) fails with ErrVersionConflict

Request A: MarkProcessed(evt-1) succeeds, event_id written
Request B: MarkProcessed(evt-1) fails with ErrDuplicateEvent

Request A: Return 200 with state=Paid, version=1
Request B: Return 200 with message="event already processed"
```

Both callers see 200. One sees the new state, one sees a message. This is correct behavior for idempotent operations.

If the caller retries Request B, it will see `IsProcessed == true` on the second attempt and exit early with 200. It never attempts the transition twice.
