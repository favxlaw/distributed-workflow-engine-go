# Concurrent Update Sequence Diagram

## Request A Succeeds, Request B Retries

```
Timeline:

T1: Request A arrives
    ├─ GetOrder("order-123")
    │  └─ Reads: state=Created, version=0
    │
    Request B arrives (simultaneous)
    ├─ GetOrder("order-123")
    │  └─ Reads: state=Created, version=0
    │
    │  (Both now have version=0 in memory)

T2: Request A attempts to write
    ├─ TransitionOrder(order, Paid)
    │  └─ UPDATE orders SET state=Paid, version=1
    │      WHERE id=order-123 AND version=0
    │
    │  ✅ Condition satisfied (current version IS 0)
    │  └─ SUCCESS: state=Paid, version=1
    │     Increments version in DB
    │
    Request B attempts to write
    ├─ TransitionOrder(order, Paid)
    │  └─ UPDATE orders SET state=Paid, version=2
    │      WHERE id=order-123 AND version=0
    │
    │  ❌ Condition NOT satisfied (current version is now 1, not 0)
    │  └─ ConditionalCheckFailedException
    │     Caught and converted to ErrVersionConflict

T3: Request A returns to caller
    ├─ HTTP 200 OK
    └─ Body: {id: "order-123", state: "Paid", version: 1}

    Request B's handler sees ErrVersionConflict
    ├─ HTTP 409 Conflict
    └─ Body: {error: "version conflict, retry"}

T4: Caller sees 409 Conflict
    ├─ Implements backoff (exponential, jittered)
    │
    └─ Retries with fresh GetOrder

T5: Retry Request B
    ├─ GetOrder("order-123")
    │  └─ Reads: state=Paid, version=1 (UPDATED)
    │
    └─ TransitionOrder(order, Packed)
       └─ UPDATE orders SET state=Packed, version=2
           WHERE id=order-123 AND version=1
       │
       │  ✅ Condition satisfied (current version IS 1)
       │  └─ SUCCESS: state=Packed, version=2

T6: Retry returns to caller
    ├─ HTTP 200 OK
    └─ Body: {id: "order-123", state: "Packed", version: 2}
```

## What This Proves

| Scenario | Without Optimistic Locking | With Optimistic Locking |
|----------|---------------------------|------------------------|
| Request A writes | ✅ Success | ✅ Success |
| Request B writes | ✅ **ERROR**: Both succeed | ❌ Conflict (correct) |
| Final version | ❌ **1** (should be 2) | ✅ 2 (correct) |
| Final state | ❌ Unknown/Corrupted | ✅ Consistent |
| Data integrity | ❌ LOST UPDATE | ✅ Preserved |

## The Critical Condition Expression

```go
ConditionExpression: awsString("#v = :expected_version")

ExpressionAttributeNames: map[string]string{
    "#v": "version",
}

ExpressionAttributeValues: map[string]types.AttributeValue{
    ":expected_version": {Value: "0"}, // What the client read
}
```

**Why the condition matters:**

Without it:
```
UPDATE orders SET state=Paid WHERE id=order-123
// Both requests succeed, last write wins
```

With it:
```
UPDATE orders SET state=Paid WHERE id=order-123 AND version=0
// Only the first request with version 0 succeeds
// The second fails because version is now 1
```

The entire correctness guarantee hinges on this three-line condition.

## Retry Strategy Recommendation

Exponential backoff with jitter:

```
Attempt 1: Retry immediately
Attempt 2: Wait 10ms + random(0-10ms)
Attempt 3: Wait 50ms + random(0-50ms)
Attempt 4: Wait 250ms + random(0-250ms)
Attempt 5+: Wait MAX(2000ms) + random(0-2000ms)

Max retries: 5-10
```

This prevents retry storms and gives other requests time to complete.

## Common Misconception

**Question:** "Can I just check if version changed, then update?"

**Answer:** No. That is also not atomic:

```go
// WRONG - TOCTOU race:
currentVersion := getVersion()
if currentVersion == expectedVersion {
    updateVersion()  // But version might have changed between the check and the update
}
```

DynamoDB's conditional write is atomic. A normal UPDATE plus IF is not.

That is why the condition must be in the same UpdateItem call. DynamoDB evaluates the condition and applies the update atom ically.
