package entity

import "errors"

// Errors the storage returns and the HTTP layer maps onto status codes.
var (
	// ErrNotFound means the record asked for does not exist, as opposed to a
	// storage failure, which must not be reported as 404.
	ErrNotFound = errors.New("not found")
	// ErrConflict means the record exists already.
	ErrConflict = errors.New("already exists")
	// ErrInProgress means the same message is being delivered right now by
	// another call with the same dedup key.
	ErrInProgress = errors.New("delivery of this message is already running")
	// ErrForbidden means the sender has no right to the topic it asked for.
	ErrForbidden = errors.New("topic not allowed for this sender")
	// ErrInvalid means the request itself is wrong: an unknown value or a
	// missing field the operation cannot do without.
	ErrInvalid = errors.New("invalid request")
)
