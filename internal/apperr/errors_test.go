package apperr_test

import (
	"context"
	"errors"
	"testing"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
)

func TestPublicCodeAndHTTPStatus(t *testing.T) {
	tests := []struct {
		kind   error
		code   string
		status int
	}{
		{apperr.ErrUnauthenticated, "UNAUTHENTICATED", 401},
		{apperr.ErrForbidden, "FORBIDDEN", 403},
		{apperr.ErrNotFound, "NOT_FOUND", 404},
		{apperr.ErrVersion, "VERSION_CONFLICT", 409},
		{apperr.ErrConflict, "RESOURCE_CONFLICT", 409},
		{apperr.ErrExpired, "EXPIRED", 422},
		{apperr.ErrCancelled, "CANCELLED", 408},
		{apperr.ErrInvalid, "INVALID_ARGUMENT", 422},
		{errors.New("unknown"), "INTERNAL", 500},
	}
	for _, test := range tests {
		err := apperr.Wrap(test.kind, "operation", "message", context.Canceled)
		if code := apperr.PublicCode(err); code != test.code {
			t.Errorf("PublicCode(%v)=%s want %s", test.kind, code, test.code)
		}
		if status := apperr.HTTPStatus(err); status != test.status {
			t.Errorf("HTTPStatus(%v)=%d want %d", test.kind, status, test.status)
		}
	}
}

func TestErrorPreservesKindAndCause(t *testing.T) {
	cause := errors.New("database unavailable")
	err := apperr.Wrap(apperr.ErrConflict, "window.reserve", "resource conflict", cause)
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatal("wrapped error lost public kind")
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrapped error lost original cause")
	}
	if got := err.Error(); got == "" {
		t.Fatal("wrapped error should have diagnostic text")
	}
	plain := apperr.New(apperr.ErrInvalid, "input.decode", "bad request")
	if !errors.Is(plain, apperr.ErrInvalid) {
		t.Fatal("new error lost kind")
	}
	if errors.Is(plain, cause) {
		t.Fatal("plain error unexpectedly contains cause")
	}
}
