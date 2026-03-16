package storage

import "fmt"

type ErrOrderNotFound struct{ ID string }

type ErrVersionConflict struct {
	ID              string
	ExpectedVersion int
}

type ErrOrderAlreadyExists struct{ ID string }

func (e ErrOrderNotFound) Error() string {
	return fmt.Sprintf("order not found: %s", e.ID)
}

func (e ErrVersionConflict) Error() string {
	return fmt.Sprintf("version conflict for order %s: expected version %d", e.ID, e.ExpectedVersion)
}

func (e ErrOrderAlreadyExists) Error() string {
	return fmt.Sprintf("order already exists: %s", e.ID)
}
