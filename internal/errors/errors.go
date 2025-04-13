package errors

import (
	"errors"
	"fmt"
)

// Standard error types for the application
var (
	// ErrNotFound indicates a requested resource wasn't found
	ErrNotFound = errors.New("resource not found")

	// ErrAlreadyExists indicates a resource already exists
	ErrAlreadyExists = errors.New("resource already exists")

	// ErrInvalidInput indicates invalid user input
	ErrInvalidInput = errors.New("invalid input")

	// ErrInternal indicates an internal server error
	ErrInternal = errors.New("internal error")
)

// NotFound creates a formatted "not found" error
func NotFound(resource, id string) error {
	return fmt.Errorf("%w: %s with ID %s", ErrNotFound, resource, id)
}

// AlreadyExists creates a formatted "already exists" error
func AlreadyExists(resource, id string) error {
	return fmt.Errorf("%w: %s with ID %s", ErrAlreadyExists, resource, id)
}

// InvalidInput creates a formatted "invalid input" error
func InvalidInput(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, reason)
}

// Internal creates a formatted "internal error" error
func Internal(err error) error {
	return fmt.Errorf("%w: %v", ErrInternal, err)
}

// Is checks if an error is a specific error
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// IsNotFound checks if an error is a "not found" error
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsAlreadyExists checks if an error is an "already exists" error
func IsAlreadyExists(err error) bool {
	return errors.Is(err, ErrAlreadyExists)
}

// IsInvalidInput checks if an error is an "invalid input" error
func IsInvalidInput(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}

// IsInternal checks if an error is an "internal error" error
func IsInternal(err error) bool {
	return errors.Is(err, ErrInternal)
}
