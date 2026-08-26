package httpapi

import "github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"

func serviceAuthenticationError() error {
	return apperr.New(apperr.ErrUnauthenticated, "http.authentication", "bearer token is required")
}
func panicError() error { return apperr.New(nil, "http.panic", "handler panicked") }
