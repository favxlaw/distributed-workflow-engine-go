# State Machine Diagram

## States and Transitions

```
                         ┌──────────────────────┐
                         │     Created          │
                         │  (initial state)     │
                         └──────┬───────────────┘
                                │
                    ┌───────────┴────────────┐
                    │                        │
                    ▼                        ▼
            ┌────────────────┐       ┌──────────────┐
            │      Paid      │       │  Cancelled   │
            │                │       │  (terminal)  │
            └────────┬───────┘       └──────────────┘
                     │
                     │
                     ▼
            ┌────────────────┐       ┌──────────────┐
            │     Packed     │       │  Cancelled   │
            │                │───────│  (terminal)  │
            └────────┬───────┘       └──────────────┘
                     │
                     │
                     ▼
            ┌────────────────┐
            │    Shipped     │
            │                │
            └────────┬───────┘
                     │
                     │
                     ▼
            ┌────────────────────┐
            │    Delivered       │
            │   (terminal state) │
            └────────────────────┘
```

## Transition Matrix

| From              | To (Allowed)         | Meaning                                    |
|-------------------|----------------------|--------------------------------------------|
| **Created**       | Paid, Cancelled      | Order confirmed or explicitly cancelled     |
| **Paid**          | Packed, Cancelled    | Initiate fulfilment or cancel if needed    |
| **Packed**        | Shipped              | Only forward motion allowed                |
| **Shipped**       | Delivered            | One step to completion                     |
| **Delivered**     | (none)               | Terminal state, no transitions possible    |
| **Cancelled**     | (none)               | Terminal state, no transitions possible    |

## Terminal States

Once an order reaches **Delivered** or **Cancelled**, no more transitions are allowed.

Attempting to transition out of a terminal state:

```
order.State = Delivered
newState = Paid

IsValidTransition(Delivered, Paid) -> false

Result: ErrInvalidTransition{From: Delivered, To: Paid}
HTTP Status: 422 Unprocessable Entity
```

## Key Constraints

1. **Linear progression**: Created → Paid → Packed → Shipped → Delivered
   - Cannot skip steps
   - Cannot go backwards
   - Cannot jump to unrelated states

2. **Exit valve**: Any non-terminal state can transition to Cancelled
   - Cancellation is always possible until delivery is complete

3. **Irreversibility**: Terminal states are permanent
   - Prevents "un-delivering" or "un-cancelling" orders
   - Reflects business reality: delivered orders stay delivered

## Enforcement Points

The FSM is enforced at two points:

1. **Storage layer** (`internal/storage/orders.go`):
   ```go
   if !workflow.IsValidTransition(order.State, newState) {
       return workflow.ErrInvalidTransition{...}
   }
   ```

2. **Workflow layer** (`internal/workflow/transitions.go`):
   ```go
   func IsValidTransition(from, to OrderState) bool {
       // Lookup in allowedTransitions map
   }
   ```

Both layers check. If one is compromised or buggy, the other catches it.
