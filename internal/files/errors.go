package files

import "errors"

var (
	ErrInvalidPath  = errors.New("files: invalid path")
	ErrUnsafeEntry  = errors.New("files: unsafe archive entry")
	ErrNotRegular   = errors.New("files: not a regular file")
	ErrPathNotFound = errors.New("files: path not found")
	ErrTooLarge     = errors.New("files: exceeds configured limit")
	ErrNotDirectory = errors.New("files: not a directory")
)
