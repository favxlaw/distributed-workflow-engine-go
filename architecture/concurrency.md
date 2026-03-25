# Optimistic Locking and Concurrent Updates

## The Concurrent Update Problem

Imagine two HTTP requests arrive for the same order simultaneously:

```
Request A: GET /orders/order-123 -> reads version=0, state=Created
Request B: GET /orders/order-123 -> reads version=0, state=Created

(Both now attempt transitionto Paid with version=0)

Request A: SET state=Paid WHERE version=0 -> SUCCESS, version becomes 1
Request B: SET state=Paid WHERE version=0 -> DEPENDS ON PROTECTION
```

**Without protection**: Request B succeeds anyway. Both requests succeeded. Version is 1, but it should be 2. You have lost an update. The database is inconsistent.

**With optimistic locking**: Request B fails with a version conflict. The caller knows an update happened concurrently and must retry with a fresh read.

## Optimistic vs. Pessimistic Locking

### Pessimistic Locking

"Lock early, hold until safe":

```
Request A: LOCK row order-123
Request A: UPDATE state=Paid
Request A: UNLOCK
Request B: LOCK order-123 (waits for A)
Request B: UPDATE state=Packed
Request B: UNLOCK
```

Advantages:
- Conflicts are impossible during the critical section
- No retries needed

Disadvantages:
- Contention: every concurrent request waits
- Deadlocks possible with multiple rows
- Throughput limited by lock hold time
- Requires shared infrastructure (lock manager)

### Optimistic Locking

"Read, try to write, detect conflicts":

```
Request A: READ version=0
Request B: READ version=0

Request A: WRITE IF version=0 -> SUCCESS, version=1
Request B: WRITE IF version=0 -> FAIL (ErrVersionConflict)

Request B: READ again (version=1)
Request B: WRITE IF version=1 -> SUCCESS, version=2
```

Advantages:
- No lock contention
- No deadlocks
- Better throughput under low contention
- Reads never block

Disadvantages:
- Under very high contention, many retries required (retry storms)
- Caller must handle ErrVersionConflict

### Why Optimistic Locking Is Right Here

This order processing system expects:

- **Low to moderate contention**: Most orders are processed independently. A single order is touched by one service at a time.
- **No shared locks**: Orders are independent. Locking one does not block another.
- **High throughput requirements**: Thousands of orders processed concurrently. Lock contention would tank throughput.

Pessimistic locking would create a bottleneck: every transition waits for the previous one to complete. Optimistic locking allows all requests to proceed, and conflicting ones simply retry.

## How DynamoDB Conditional Writes Work

In `internal/storage/orders.go`, the `TransitionOrder` method uses:

```go
ConditionExpression: "#v = :expected_version"
```

Where:
- `#v` = "version" attribute name
- `:expected_version` = the version read by the caller

DynamoDB's semantics:

1. Atomically read the current version
2. Compare with `:expected_version`
3. If equal: apply the update
4. If not equal: return `ConditionalCheckFailedException`

This is atomic. No TOCTOU (time-of-check-time-of-use) races.

## Conflict Handling

When a concurrent request loses the race:

```go
if err := store.TransitionOrder(ctx, order, workflow.Paid); err != nil {
    var versionErr storage.ErrVersionConflict
    if errors.As(err, &versionErr) {
        // Handle conflict
    }
}
```

The caller sees `ErrVersionConflict` and should:

1. Fetch the order again (to get the new version)
2. Attempt the transition again with the new version
3. If it fails again, back off and retry later

This is the contract: optimistic locking requires retry logic.

The HTTP layer surfaces this as a 409 response. The client is responsible for implementing backoff and retry.

## The Concurrency Test

Location: `internal/storage/concurrency_test.go`

The test proves optimistic locking works:

```go
func TestConcurrentTransitionOnlyOneSucceeds(t *testing.T) {
    // Create order with state=Created, version=0
    order := &Order{ID: "order-concurrent", State: Created}
    store.SaveOrder(ctx, order)
    
    // Two goroutines fetch and transition simultaneously
    for i := 0; i < 2; i++ {
        go func() {
            fetchedOrder := store.GetOrder(ctx, "order-concurrent")
            store.TransitionOrder(ctx, fetchedOrder, Paid)
        }()
    }
    
    // Collect results:
    // - 1 success (TransitionOrder returns nil)
    // - 1 conflict (TransitionOrder returns ErrVersionConflict)
    
    // Verify final state:
    // - state == Paid (not Created, not corrupted)
    // - version == 1 (not 2, only one write succeeded)
}
```

Without optimistic locking:
- Both goroutines would succeed
- Version would be 2 (both incremented it)
- Data would be inconsistent

With optimistic locking:
- Exactly one succeeds
- Version is 1
- Data is consistent

This test runs against real DynamoDB Local, so it is not a mock. It proves the system works under actual concurrent load.

## Trade-Offs

Optimistic locking is ideal for:
- Low contention workloads
- Independent entities (orders don't lock other orders)
- High throughput requirements

Optimistic locking is poor for:
- Very high contention on single rows (every request retries)
- Short lock hold times (retry overhead dominates)
- Scenarios where blocking is acceptable (real-time collaboration)

For this system, the assumption is that order contentions are rare and order processing is latency-tolerant (seconds, not milliseconds). Under those assumptions, optimistic locking is almost always better.
