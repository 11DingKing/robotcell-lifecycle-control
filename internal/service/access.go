package service

import (
	"context"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
)

type principalKey struct{}

func ContextWithPrincipal(ctx context.Context, principal identity.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (identity.Principal, error) {
	principal, ok := ctx.Value(principalKey{}).(identity.Principal)
	if !ok || principal.UserID <= 0 {
		return identity.Principal{}, apperr.New(apperr.ErrUnauthenticated, "service.principal", "authenticated principal is required")
	}
	return principal, nil
}

func RequireRole(ctx context.Context, roles ...identity.Role) (identity.Principal, error) {
	principal, err := PrincipalFromContext(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	if !principal.HasAny(roles...) {
		return identity.Principal{}, apperr.New(apperr.ErrForbidden, "service.require_role", "role cannot perform this action")
	}
	return principal, nil
}
