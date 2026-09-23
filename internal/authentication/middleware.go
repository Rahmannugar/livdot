package authentication

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Rahmannugar/livdot/internal/infra/httpapi"
	"github.com/gin-gonic/gin"
)

const identityContextKey = "livdot.authentication.identity"

// resolves a raw session token to an authenticated account.
type SessionResolver interface {
	Authenticate(ctx context.Context, rawToken string) (Identity, error)
}

// rejects requests without a valid bearer token and stashes the resolved
// identity on the request context.
func RequireSession(resolver SessionResolver) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		rawToken, ok := bearerToken(ctx)
		if !ok {
			writeMiddlewareError(ctx, http.StatusUnauthorized, "unauthenticated",
				"a valid session token is required")
			return
		}
		identity, err := resolver.Authenticate(ctx.Request.Context(), rawToken)
		if errors.Is(err, ErrUnauthenticated) {
			writeMiddlewareError(ctx, http.StatusUnauthorized, "unauthenticated",
				"a valid session token is required")
			return
		}
		if err != nil {
			writeMiddlewareFailure(ctx, err, http.StatusInternalServerError, "authentication_unavailable",
				"authentication could not be completed")
			return
		}
		ctx.Set(identityContextKey, identity)
		ctx.Next()
	}
}

// only allows the listed roles. must run after RequireSession.
func RequireRole(roles ...Role) gin.HandlerFunc {
	allowed := make(map[Role]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(ctx *gin.Context) {
		identity, ok := IdentityFrom(ctx)
		if !ok {
			writeMiddlewareError(ctx, http.StatusUnauthorized, "unauthenticated",
				"a valid session token is required")
			return
		}
		if _, permitted := allowed[identity.Role]; !permitted {
			writeMiddlewareError(ctx, http.StatusForbidden, "forbidden",
				"this account cannot perform the requested action")
			return
		}
		ctx.Next()
	}
}

// the identity attached by RequireSession.
func IdentityFrom(ctx *gin.Context) (Identity, bool) {
	value, exists := ctx.Get(identityContextKey)
	if !exists {
		return Identity{}, false
	}
	identity, ok := value.(Identity)
	return identity, ok
}

func bearerToken(ctx *gin.Context) (string, bool) {
	parts := strings.Fields(ctx.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

func writeMiddlewareError(ctx *gin.Context, status int, code, message string) {
	ctx.Abort()
	httpapi.WriteError(ctx, nil, status, code, message, "")
}

func writeMiddlewareFailure(ctx *gin.Context, err error, status int, code, message string) {
	ctx.Abort()
	httpapi.WriteError(ctx, err, status, code, message, "")
}
