package identity

import "context"

type contextKey string

const principalKey contextKey = "identity-principal"

type Principal struct {
	User      User
	Session   Session
	SessionID string
	CSRFToken string
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey).(Principal)
	return principal, ok
}
