// SPDX-License-Identifier: AGPL-3.0-only
package errors

import (
	"fmt"
	"testing"
)

func TestNotFound(t *testing.T) {
	err := NotFound("task", "123")
	if !IsNotFound(err) {
		t.Error("NotFound error should be detectable with IsNotFound")
	}
	expectedMsg := "resource not found: task with ID 123"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestAlreadyExists(t *testing.T) {
	err := AlreadyExists("task", "123")
	if !IsAlreadyExists(err) {
		t.Error("AlreadyExists error should be detectable with IsAlreadyExists")
	}
	expectedMsg := "resource already exists: task with ID 123"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestInvalidInput(t *testing.T) {
	reason := "missing required field"
	err := InvalidInput(reason)
	if !IsInvalidInput(err) {
		t.Error("InvalidInput error should be detectable with IsInvalidInput")
	}
	expectedMsg := "invalid input: " + reason
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestInternal(t *testing.T) {
	originalErr := fmt.Errorf("database connection failed")
	err := Internal(originalErr)
	if !IsInternal(err) {
		t.Error("Internal error should be detectable with IsInternal")
	}
	expectedMsg := "internal error: database connection failed"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestErrorIs(t *testing.T) {
	// Test with direct error
	err := ErrNotFound
	if !Is(err, ErrNotFound) {
		t.Error("Direct error should match with Is")
	}

	// Test with wrapped error
	wrappedErr := fmt.Errorf("wrapped: %w", ErrNotFound)
	if !Is(wrappedErr, ErrNotFound) {
		t.Error("Wrapped error should match with Is")
	}

	// Test with double-wrapped error (through our helper)
	doubleWrapped := NotFound("task", "123")
	if !Is(doubleWrapped, ErrNotFound) {
		t.Error("Double-wrapped error should match with Is")
	}
}

// Define a custom error type for the TestErrorAs test
type customError struct {
	msg string
}

// Error implements the error interface
func (e *customError) Error() string {
	return e.msg
}

func TestErrorAs(t *testing.T) {
	// Create an instance of the custom error
	original := &customError{msg: "custom error"}

	// Wrap it
	wrapped := fmt.Errorf("wrapped: %w", original)

	// Try to extract the original error
	var target *customError
	if !As(wrapped, &target) {
		t.Error("As should extract the original error from wrapped error")
	}

	if target.msg != original.msg {
		t.Errorf("Expected extracted error message '%s', got '%s'", original.msg, target.msg)
	}
}
