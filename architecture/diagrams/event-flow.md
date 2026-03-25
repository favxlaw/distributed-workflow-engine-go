# Idempotent Event Flow Diagram

## TransitionOrder Handler Flow

```
HTTP Request arrives
│
├─ Body:
│  ├─ id: "order-123"
│  ├─ event_id: "evt-payment-001"
│  └─ new_state: "Paid"
│
└─ Handler begins

    ┌─────────────────────────────────────────┐
    │ STEP 1: Idempotency Check               │
    │ Check if this event was already process │
    └─────────────────────────────────────────┘
    │
    └─ IsProcessed(event_id)
       │
       ├─ Query processed_events table
       │  └─ WHERE event_id = "evt-payment-001"
       │
       ├─ If found:
       │  └─ ✓ Event was already processed
       │     │
       │     └─ Return HTTP 200 OK
       │        {message: "event already processed"}
       │        (Exit here - no transition)
       │
       └─ If not found:
          └─ ✓ First time seeing this event
             └─ Continue to Step 2

    ┌─────────────────────────────────────────┐
    │ STEP 2: Fetch Current Order             │
    │ Get fresh state from database           │
    └─────────────────────────────────────────┘
    │
    └─ GetOrder(id)
       │
       ├─ Query orders table
       │  └─ WHERE id = "order-123"
       │
       ├─ If not found:
       │  └─ ✗ Order does not exist
       │     │
       │     └─ Return HTTP 404 Not Found
       │        {error: "order not found"}
       │        (Exit here)
       │
       └─ If found:
          └─ ✓ Order exists
             ├─ state = "Created"
             ├─ version = 0
             └─ Continue to Step 3

    ┌─────────────────────────────────────────┐
    │ STEP 3: Transition Order                │
    │ Apply state change with version check   │
    └─────────────────────────────────────────┘
    │
    └─ TransitionOrder(order, "Paid")
       │
       ├─ Validate transition
       │  └─ IsValidTransition(Created, Paid)
       │     └─ ✓ Allowed
       │
       ├─ Update with condition
       │  └─ UPDATE orders
       │      SET state="Paid", version=1
       │      WHERE id="order-123" AND version=0
       │
       ├─ If condition fails:
       │  ├─ ✗ Version changed (concurrent update)
       │  │  │
       │  │  └─ Return HTTP 409 Conflict
       │  │     {error: "version conflict, retry"}
       │  │     (Exit here - no mark processed)
       │  │
       │  └─ (No cleanup needed; event not marked)
       │
       └─ If condition succeeds:
          └─ ✓ Order transitioned
             ├─ state = "Paid"
             ├─ version = 1
             └─ Continue to Step 4

    ┌─────────────────────────────────────────┐
    │ STEP 4: Mark Event as Processed         │
    │ Record this event_id to prevent replays │
    └─────────────────────────────────────────┘
    │
    └─ MarkProcessed(event_id)
       │
       ├─ Write to processed_events table
       │  └─ PutItem {
       │       event_id: "evt-payment-001",
       │       expires_at: <unix timestamp 24h from now>
       │     }
       │     Condition: attribute_not_exists(event_id)
       │
       ├─ If condition fails:
       │  ├─ ✗ Race condition: another request marked it
       │  │  │
       │  │  └─ This is ErrDuplicateEvent
       │  │
       │  └─ Return HTTP 200 OK
       │     {message: "event already processed"}
       │     (Order IS transitioned; event marked by other request)
       │
       └─ If condition succeeds:
          └─ ✓ Event recorded
             └─ Continue to Step 5

    ┌─────────────────────────────────────────┐
    │ STEP 5: Return Success                  │
    │ Respond with new order state            │
    └─────────────────────────────────────────┘
    │
    └─ Return HTTP 200 OK
       {
         id: "order-123",
         state: "Paid",
         version: 1
       }
```

## Concurrent Requests with Same Event ID

```
Request A                             Request B
(same event_id)

│                                    │
├─ IsProcessed → false              ├─ IsProcessed → false
│                                    │
├─ TransitionOrder → success ✓      ├─ TransitionOrder → success ✓
│  (version 0→1)                     │  (reads version 0, writes version 1)
│                                    │
├─ MarkProcessed("evt-1")           ├─ MarkProcessed("evt-1")
│  condition: attr_not_exists        │
│  → SUCCESS                         │  → FAIL (ConditionalCheckFailedException)
│  (writes first)                    │  (version 1 already exists)
│                                    │
└─ Return 200                        └─ Return 200
  {state: Paid, version: 1}            {message: "event already processed"}


Both return 200. One has state, one has a message. Both are idempotent.
On third request:
  └─ IsProcessed → true
     └─ Return 200 {message: "event already processed"}
        (never attempts transition again)
```

## Why Sequence Order Matters

```
❌ WRONG:
    1. MarkProcessed(evt-id)    ← Marked first
    2. TransitionOrder(...)     ← Then transition
    
    If TransitionOrder fails:
      - Event is marked as processed
      - Order is NOT transitioned
      - Retry sees "event already processed" and exits
      - Order never transitions (SILENT DATA LOSS)

✅ CORRECT:
    1. TransitionOrder(...)     ← Transition first
    2. MarkProcessed(evt-id)    ← Mark second
    
    If MarkProcessed fails:
      - Order IS transitioned
      - Event is NOT marked
      - Retry sees "not processed" and retries
      - TransitionOrder fails with ErrVersionConflict
      - Caller knows something went wrong (NOT SILENT)
```

The correct sequence ensures failures are visible and retryable.

## Error Paths (HTTP Status Codes)

| Scenario | HTTP Status | Idempotent |
|----------|------------|-----------|
| First successful transition | 200 OK | ✓ |
| Duplicate event (already processed) | 200 OK | ✓ |
| Event not found | 404 Not Found | N/A |
| Invalid transition | 422 Unprocessable Entity | ✓ |
| Version conflict (concurrent update) | 409 Conflict | N/A (caller retries with fresh read) |
| DynamoDB failure | 500 Internal Server Error | ✓ (repeat is safe) |

**Idempotent**: Repeating the request produces the same final state.

**Not idempotent but safe**: Callers must handle—409 signals "retry with fresh read", 500 signals "try again later".
