package apperr

import (
	"errors"
	"fmt"
)

var (
	ErrInvalid         = errors.New("invalid input")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrVersion         = errors.New("version conflict")
	ErrExpired         = errors.New("expired")
	ErrCancelled       = errors.New("cancelled")
)

type Error struct {
	Kind    error
	Op      string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

func (e *Error) Unwrap() error {
	if e.Cause != nil {
		return errors.Join(e.Kind, e.Cause)
	}
	return e.Kind
}

func New(kind error, op, message string) error {
	return &Error{Kind: kind, Op: op, Message: message}
}

func Wrap(kind error, op, message string, cause error) error {
	return &Error{Kind: kind, Op: op, Message: message, Cause: cause}
}

func PublicCode(err error) string {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return "UNAUTHENTICATED"
	case errors.Is(err, ErrForbidden):
		return "FORBIDDEN"
	case errors.Is(err, ErrNotFound):
		return "NOT_FOUND"
	case errors.Is(err, ErrVersion):
		return "VERSION_CONFLICT"
	case errors.Is(err, ErrConflict):
		return "RESOURCE_CONFLICT"
	case errors.Is(err, ErrExpired):
		return "EXPIRED"
	case errors.Is(err, ErrCancelled):
		return "CANCELLED"
	case errors.Is(err, ErrInvalid):
		return "INVALID_ARGUMENT"
	default:
		return "INTERNAL"
	}
}

func HTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return 401
	case errors.Is(err, ErrForbidden):
		return 403
	case errors.Is(err, ErrNotFound):
		return 404
	case errors.Is(err, ErrConflict), errors.Is(err, ErrVersion):
		return 409
	case errors.Is(err, ErrExpired), errors.Is(err, ErrInvalid):
		return 422
	case errors.Is(err, ErrCancelled):
		return 408
	default:
		return 500
	}
}
