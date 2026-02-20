package noplane

import "fmt"

// ConflictError indicates a 409 Conflict response from the API.
type ConflictError struct {
	Name string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("conflict: plane with name %q already exists", e.Name)
}

// NewConflictError returns a new ConflictError.
func NewConflictError(name string) error {
	return &ConflictError{Name: name}
}

// IsConflict returns true if err is a ConflictError.
func IsConflict(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*ConflictError)
	return ok
}

// NotFoundError indicates a 404 Not Found response from the API.
type NotFoundError struct {
	ID string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("not found: plane %q does not exist", e.ID)
}

// NewNotFoundError returns a new NotFoundError.
func NewNotFoundError(id string) error {
	return &NotFoundError{ID: id}
}

// IsNotFound returns true if err is a NotFoundError.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*NotFoundError)
	return ok
}
