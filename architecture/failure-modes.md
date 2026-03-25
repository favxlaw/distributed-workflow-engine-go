# Failure Modes and Error Handling

This document covers what happens when things go wrong and how the system responds.

## 1. Duplicate Event Delivery

### What Happens

A client sends a transition request with `event_id: "evt-payment-123"`. The server processes it successfully. The network response is lost. The client retries with the same `event_id`.

### How Idempotency Handles It

```
Request 1:
  IsProcessed("evt-payment-123") -> false
  TransitionOrder(...) -> SUCCESS, state=Paid, version=1
  MarkProcessed("evt-payment-123") -> SUCCESS
  Return 200 {state=Paid, version=1}

Request 2 (retry):
  IsProcessed("evt-payment-123") -> true
  Return 200 "event already processed"
```

Request 2 never attempts the transition. It exits early with a 200 response.

### What the Caller Sees

Both requests return 200 OK. Request 1 contains the new order state. Request 2 contains a message indicating the event was already processed. The caller cannot distinguish them by status code, but it does not matter—the final state is correct in both cases.

### Is This Correct?

Yes. In an idempotent system, replaying the same request should produce the same overall result. The order is Paid. Whether that state was returned in the first or second response is not the caller's concern.

If the caller is paranoid (reasonable in distributed systems), it can:

1. Send the request
2. Get 200 back
3. Query the order state separately to confirm

Or, it can use deterministic event IDs (e.g., hash of the logical operation) so retries are guaranteed idempotent.

## 2. Concurrent State Transition Conflict

### What Happens

Two services process the same order simultaneously. Both read version 0. Both attempt to transition to Paid. The first one wins and increments version to 1. The second one's conditional write fails.

```
Service A: READ version=0
Service B: READ version=0

Service A: UPDATE WHERE version=0 -> SUCCESS (version=1)
Service B: UPDATE WHERE version=0 -> FAIL (ConditionalCheckFailedException)
```

### What ErrVersionConflict Means

It means: "The data changed between when you read it and when you attempted to write it. Your write is unsafe. Retry with a fresh read."

### Expected Retry Behavior

```
Service B:
  First attempt: TransitionOrder fails with ErrVersionConflict
  Receives 409 Conflict from HTTP layer
  
  Retry logic (in caller's code):
    Wait (exponential backoff)
    READ order again
    retry TransitionOrder
    Now version=1, and if state allows, transition succeeds
```

The caller is responsible for implementing backoff. A naive loop that retries immediately will create a retry storm.

### Should the Service Implement Retries?

No. This service does not retry. It surfaces errors to the caller and lets them decide. This keeps the service simple and allows each caller to apply their own backoff policy.

If a service is critical and cannot afford caller-side retries, a sidecar or API gateway can implement retries transparently. But that is an architectural choice, not this service's responsibility.

## 3. Transition to Invalid State

### What Happens

A client sends a request to transition an order from Created directly to Delivered, skipping all intermediate states.

### Where It Is Caught

In `internal/storage/orders.go`, before any database operation:

```go
if !workflow.IsValidTransition(order.State, newState) {
    return workflow.ErrInvalidTransition{From: order.State, To: newState}
}
```

DynamoDB is never touched. The request fails fast and locally.

### What the Caller Sees

HTTP 422 Unprocessable Entity:

```json
{
  "error": "invalid state transition"
}
```

Status 422 signals: "Your request is not semantically valid. Do not retry. Fix your request."

### Why Two Layers Check This

The workflow layer checks it first (in `internal/workflow/transitions.go`). The storage layer checks it again. This is redundant by design—it ensures invalid transitions cannot slip through due to a bug in one layer.

## 4. DynamoDB Unavailable

### What Happens

DynamoDB is unreachable or returns a 500 ServiceUnavailableException. All database operations fail.

### What Errors Surface

DynamoDB SDK returns a specific error type (e.g., `ServiceUnavailableException`). The storage layer does not convert it. It propagates to the HTTP layer:

```go
if err != nil {
    writeError(w, http.StatusInternalServerError, "internal server error")
}
```

The caller receives: HTTP 500 Internal Server Error with a generic message. No details are leaked.

### What This System Does NOT Do

This service has no built-in retry logic for database failures. It does not:

- Retry transient failures automatically
- Use circuit breakers
- Fall back to a cache

If DynamoDB is down, orders cannot be transitioned. Period.

### Who Handles This

The caller or infrastructure layer:

- **Caller-side**: Implement retries with backoff, fall back to cached data
- **Infrastructure-side**: Use a service mesh (Istio) or gateway (Kong) that retries transparently
- **Operations**: Ensure DynamoDB availability through proper scaling and failover

## 5. Order Not Found

### What Happens

A client sends a transition request for an order ID that does not exist.

### How It Is Handled

`GetOrder` in the storage layer queries DynamoDB:

```go
resp, err := client.GetItem(ctx, &dynamodb.GetItemInput{...})
if resp.Item == nil || len(resp.Item) == 0 {
    return nil, ErrOrderNotFound{ID: id}
}
```

### What the Caller Sees

HTTP 404 Not Found:

```json
{
  "error": "order not found"
}
```

Status 404 signals: "The resource does not exist. Retrying will not help."

### Why This Is Not an Error

In systems thinking, a 404 is not an error—it is a valid state. The order does not exist. This is information, not a failure.

Whether the order never existed or was deleted is irrelevant to the caller. The response is the same.

## 6. Partial Failure: Transition Succeeds but MarkProcessed Fails

### What Happens

This is the most interesting failure mode.

```
Request arrives with event_id="evt-payment-123"

IsProcessed("evt-payment-123") -> false
TransitionOrder(order, Paid) -> SUCCESS (version 0->1)
MarkProcessed("evt-payment-123") -> FAIL (DynamoDB unavailable)
```

The order is now Paid, but the event is not marked as processed.

### What Happens Next

The HTTP handler receives the error from MarkProcessed and returns 500:

```go
if err := h.events.MarkProcessed(ctx, req.EventID); err != nil {
    writeError(w, http.StatusInternalServerError, "internal server error")
}
```

The caller sees HTTP 500 and retries.

### On The Retry

```
Request arrives again with event_id="evt-payment-123"

IsProcessed("evt-payment-123") -> false (event still not marked)
TransitionOrder(order, Paid) -> FAIL (ErrVersionConflict, version is now 1)

Handler returns 409 Conflict
```

The caller receives 409, which signals "something changed" and to retry. But this time, if they retry:

```
Request arrives again with event_id="evt-payment-123"

IsProcessed("evt-payment-123") -> false (still)
TransitionOrder(order, Paid) -> FAIL (version conflict again)
```

Infinite retry loop.

### The Gap

This is a real gap in the current design. If MarkProcessed fails, the order is transitioned but the event is not recorded. On retry, TransitionOrder fails with a version conflict, forever.

There is a window where:
- The order is in the correct state (Paid)
- But the system cannot prove it is idempotent
- So retries are rejected

### How To Fix This (Out of Scope)

Option 1: Use a write-ahead log. Before TransitionOrder, write a record saying "I am about to transition this order with this event_id". After MarkProcessed, mark it complete. On startup, resume incomplete transitions.

Option 2: Accept the gap as acceptable for this service and put cleanup logic upstream. The caller checks: "Is my order in the expected state even though I got a 500?" If yes, consider it processed.

Option 3: Make MarkProcessed fail gracefully. Record processed events asynchronously instead of synchronously. This adds complexity but removes the blocking point.

For this system's scope, Option 2 is sufficient: the caller is responsible for state validation. But it is worth mentioning because it shows that even careful systems have trade-offs.

### Takeaway

Idempotency is not a boolean property. It exists on a spectrum. This system is idempotent for most scenarios. This scenario is a gap. Acknowledging gaps is more valuable than pretending perfection.
