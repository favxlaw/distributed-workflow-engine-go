package workflow

type OrderState string

const (
	Created   OrderState = "Created"
	Paid      OrderState = "Paid"
	Packed    OrderState = "Packed"
	Shipped   OrderState = "Shipped"
	Delivered OrderState = "Delivered"
	Cancelled OrderState = "Cancelled"
)
