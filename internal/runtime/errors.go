package runtime

import "errors"

var (
	ErrNotFound    = errors.New("runtime: not found")
	ErrConflict    = errors.New("runtime: conflict")
	ErrUnavailable = errors.New("runtime: unavailable")
)
