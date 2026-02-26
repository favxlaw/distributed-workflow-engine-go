package workflow

import "time"

type Order struct {
	ID        string
	State     OrderState
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}
