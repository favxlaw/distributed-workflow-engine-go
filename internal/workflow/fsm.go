package workflow

import "time"

type ErrInvalidTransition struct {
	From OrderState
	To   OrderState
}

func (e ErrInvalidTransition) Error() string {
	return "invalid transition from " + string(e.From) + " to " + string(e.To)
}

type FSM struct {
	order *Order
}

func NewFSM(o *Order) *FSM {
	return &FSM{order: o}
}

func (f *FSM) Transition(to OrderState) error {
	from := f.order.State
	if !IsValidTransition(from, to) {
		return ErrInvalidTransition{From: from, To: to}
	}

	f.order.State = to
	f.order.Version++
	f.order.UpdatedAt = time.Now()

	return nil
}

func (f *FSM) CurrentState() OrderState {
	return f.order.State
}
