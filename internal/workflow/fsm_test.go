package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestTransitionValid(t *testing.T) {
	now := time.Now()
	o := &Order{ID: "o-1", State: Created, Version: 1, CreatedAt: now, UpdatedAt: now}
	f := NewFSM(o)

	if err := f.Transition(Paid); err != nil {
		t.Fatalf("expected valid transition, got error: %v", err)
	}

	if got := f.CurrentState(); got != Paid {
		t.Fatalf("expected state %q, got %q", Paid, got)
	}

	if o.Version != 2 {
		t.Fatalf("expected version 2, got %d", o.Version)
	}

	if !o.UpdatedAt.After(now) {
		t.Fatalf("expected UpdatedAt to be updated")
	}
}

func TestTransitionInvalidReturnsTypedError(t *testing.T) {
	now := time.Now()
	o := &Order{ID: "o-2", State: Created, Version: 1, CreatedAt: now, UpdatedAt: now}
	f := NewFSM(o)

	err := f.Transition(Delivered)
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	var transErr ErrInvalidTransition
	if !errors.As(err, &transErr) {
		t.Fatalf("expected ErrInvalidTransition, got %T", err)
	}

	if transErr.From != Created || transErr.To != Delivered {
		t.Fatalf("unexpected transition error context: from=%q to=%q", transErr.From, transErr.To)
	}

	if o.State != Created {
		t.Fatalf("expected state to remain %q, got %q", Created, o.State)
	}

	if o.Version != 1 {
		t.Fatalf("expected version to remain 1, got %d", o.Version)
	}
}

func TestTransitionIncrementsVersionEachTime(t *testing.T) {
	now := time.Now()
	o := &Order{ID: "o-3", State: Created, Version: 0, CreatedAt: now, UpdatedAt: now}
	f := NewFSM(o)

	seq := []OrderState{Paid, Packed, Shipped}
	for i, to := range seq {
		if err := f.Transition(to); err != nil {
			t.Fatalf("transition %d failed: %v", i, err)
		}

		if want := i + 1; o.Version != want {
			t.Fatalf("after transition %d expected version %d, got %d", i, want, o.Version)
		}
	}
}

func TestTerminalStatesRejectAllTransitions(t *testing.T) {
	states := []OrderState{Created, Paid, Packed, Shipped, Delivered, Cancelled}
	terminals := []OrderState{Delivered, Cancelled}

	for _, from := range terminals {
		o := &Order{ID: "o-terminal", State: from, Version: 5, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		f := NewFSM(o)

		for _, to := range states {
			err := f.Transition(to)
			if err == nil {
				t.Fatalf("expected transition from terminal state %q to %q to fail", from, to)
			}

			var transErr ErrInvalidTransition
			if !errors.As(err, &transErr) {
				t.Fatalf("expected ErrInvalidTransition, got %T", err)
			}
		}
	}
}
