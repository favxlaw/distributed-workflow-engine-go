package events

import "fmt"

type ErrDuplicateEvent struct {
	EventID string
}

func (e ErrDuplicateEvent) Error() string {
	return fmt.Sprintf("duplicate event: %s", e.EventID)
}
