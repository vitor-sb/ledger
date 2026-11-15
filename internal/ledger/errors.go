package ledger

import (
	"errors"
	"fmt"
)

// Sentinel errors: callers use errors.Is to translate them (HTTP 422, 404 and 409).
var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
)

func invalidInputf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}
