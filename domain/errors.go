package domain

import "errors"

var (
	ErrNotFound         = errors.New("subscription not found")
	ErrAlreadyCancelled = errors.New("subscription already cancelled")
	ErrInvalidInput     = errors.New("invalid input")
	ErrInvalidPrice     = errors.New("price must be positive")
)
