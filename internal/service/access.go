package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/audit"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
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

func requireRoleWithAudit(ctx context.Context, database *store.Store, serviceClock clock.Clock, action, objectType string, objectID int64, requestID string, roles ...identity.Role) (identity.Principal, error) {
	principal, err := PrincipalFromContext(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	if principal.HasAny(roles...) {
		return principal, nil
	}
	return identity.Principal{}, rejectWithAudit(ctx, database, serviceClock, principal, action, objectType, objectID, requestID, map[string]any{"allowed_roles": roles})
}

func rejectWithAudit(ctx context.Context, database *store.Store, serviceClock clock.Clock, principal identity.Principal, action, objectType string, objectID int64, requestID string, details any) error {
	event, eventErr := audit.New(principal.UserID, string(principal.Role), action, objectType, strconv.FormatInt(objectID, 10), requestID, audit.ResultRejected, details, serviceClock.Now())
	if eventErr != nil {
		return fmt.Errorf("build authorization audit: %w", eventErr)
	}
	if _, eventErr = database.AppendAudit(ctx, event); eventErr != nil {
		return apperr.Wrap(apperr.ErrForbidden, "service.require_role", "role cannot perform this action and rejection audit failed", eventErr)
	}
	return apperr.New(apperr.ErrForbidden, "service.require_role", "role cannot perform this action")
}
