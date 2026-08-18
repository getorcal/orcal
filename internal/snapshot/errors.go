package snapshot

import "errors"

var (
	ErrNotFound        = errors.New("snapshot: not found")
	ErrNameTaken       = errors.New("snapshot: name already in use")
	ErrInvalidName     = errors.New("snapshot: invalid name")
	ErrNameLooksLikeID = errors.New("snapshot: name must not be a UUID")
	ErrHasChildren     = errors.New("snapshot: has descendants")
)
