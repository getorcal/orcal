package id

import "github.com/google/uuid"

func New() string {
	v7, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return v7.String()
}
