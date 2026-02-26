package workflow

var allowedTransitions = map[OrderState][]OrderState{
	Created:   {Paid, Cancelled},
	Paid:      {Packed, Cancelled},
	Packed:    {Shipped},
	Shipped:   {Delivered},
	Delivered: {},
	Cancelled: {},
}

func IsValidTransition(from, to OrderState) bool {
	next, ok := allowedTransitions[from]
	if !ok {
		return false
	}

	for _, s := range next {
		if s == to {
			return true
		}
	}

	return false
}
