package log

import "errors"

var (
	// ErrTooLarge is returned for too large log.
	ErrTooLarge = errors.New("too large log")

	// ErrInvalidKey is returned when fields contain invalid key.
	ErrInvalidKey = errors.New("invalid key")

	// ErrInvalidData is returned when fields contain invalid data.
	ErrInvalidData = errors.New("invalid data type")
)
