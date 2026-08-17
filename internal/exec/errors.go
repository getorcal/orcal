package exec

import "errors"

var ErrNotFound = errors.New("exec: not found")

var ErrWriterFailed = errors.New("exec: log writer failed")
