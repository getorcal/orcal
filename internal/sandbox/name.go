package sandbox

import (
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: name must match %s", ErrInvalidName, namePattern)
	}
	if name[len(name)-1] == '-' {
		return fmt.Errorf("%w: name must not end with a hyphen", ErrInvalidName)
	}
	if _, err := uuid.Parse(name); err == nil {
		return fmt.Errorf("%w: name must not be a UUID", ErrNameLooksLikeID)
	}
	return nil
}
