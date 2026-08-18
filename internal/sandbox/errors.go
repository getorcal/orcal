package sandbox

import "errors"

var (
	ErrNotFound          = errors.New("sandbox: not found")
	ErrNameTaken         = errors.New("sandbox: name already in use")
	ErrInvalidState      = errors.New("sandbox: invalid state transition")
	ErrInvalidName       = errors.New("sandbox: invalid name")
	ErrInvalidImage      = errors.New("sandbox: invalid image")
	ErrNameLooksLikeID   = errors.New("sandbox: name must not be a UUID")
	ErrInvalidResources  = errors.New("sandbox: invalid resource limits")
	ErrResourceExhausted = errors.New("sandbox: resource limit exceeded")
)
