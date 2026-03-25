# Finite State Machine

## What Is a State Machine

A finite state machine (FSM) is a model of behavior defined by:

1. A set of possible states
2. A set of allowed transitions between states
3. Rules that reject transitions that are not explicitly allowed

In plain English: "An order can move from Created to Paid, but not directly to Shipped. An order cannot go backward from Paid to Created."

Without a state machine, a state field is just a string. Anyone with database access can set it to any value. With a state machine, only explicitly allowed transitions are permitted.

## Why FSM Enforcement Matters

Consider an order processing system without FSM enforcement. Suppose code contains a bug:

```
if paymentSucceeded {
    order.state = "Delivered"  // BUG: Should be "Paid", not "Delivered"
}
```

Without enforcement, this bug lives in production for weeks. A customer receives their order before payment is confirmed. Revenue accounting is corrupted. Auditors find orders marked Delivered with no corresponding payment.

With FSM enforcement, this transition is rejected by `IsValidTransition` before it ever reaches the database:

```go
if !IsValidTransition(order.State, newState) {
    return ErrInvalidTransition{From: order.State, To: newState}
}
```

The bug is caught immediately. The service returns a 422 error.

## State Transition Table

| From        | Allowed Next States           |
|-------------|-------------------------------|
| Created     | Paid, Cancelled               |
| Paid        | Packed, Cancelled             |
| Packed      | Shipped                       |
| Shipped     | Delivered                     |
| Delivered   | (terminal state)              |
| Cancelled   | (terminal state)              |

## Terminal States

A terminal state is one with no outgoing transitions. Once an order reaches Delivered or Cancelled, it cannot transition further.

This reflects business logic: a delivered order cannot be "un-delivered". A cancelled order cannot be reactivated.

If code attempts to transition out of a terminal state, `IsValidTransition` returns false, and `ErrInvalidTransition` is returned to the caller:

```go
err := store.TransitionOrder(ctx, order, workflow.Paid)
// err = ErrInvalidTransition{From: Delivered, To: Paid}
```

## Two Layers of Defense

FSM enforcement happens at two layers:

### 1. The Workflow Layer

Location: `internal/workflow/transitions.go`, called by `internal/workflow/fsm.go`

```go
func IsValidTransition(from, to OrderState) bool {
    next, ok := allowedTransitions[from]
    // ...
}
```

This is called *before* any database operation. It is fast, deterministic, and fails locally with no network call.

### 2. The Storage Layer

Location: `internal/storage/orders.go`

```go
func (s *DynamoStore) TransitionOrder(ctx context.Context, order *workflow.Order, newState workflow.OrderState) error {
    if !workflow.IsValidTransition(order.State, newState) {
        return workflow.ErrInvalidTransition{From: order.State, To: newState}
    }
    // ... then update database
}
```

The same check before DynamoDB write. If somehow an invalid state slipped past the first check, the second check catches it.

Two layers of defense is not paranoia. It is appropriate for critical business logic.

## Enforcement Is Not Validation

Validation asks: "Is this request shape correct?" (checking JSON fields, data types)

Enforcement asks: "Is this state change permitted?" (checking business rules)

Both are necessary. Validation keeps garbage out. Enforcement keeps nonsense out. This system does both.
