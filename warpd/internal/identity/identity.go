// Package identity defines Warpgate web identity and authorization concepts.
package identity

import (
	"context"
	"errors"
	"slices"
)

// AdminCapability is the Tailscale capability required for Warpgate administration.
const AdminCapability = "pangobit.com/cap/warpgate-admin"

// User is an authenticated Warpgate web user.
type User struct {
	// Email is the user's stable login email.
	Email string
	// DisplayName is the human-readable display name.
	DisplayName string
	// Capabilities is the set of capabilities granted by the identity provider.
	Capabilities []string
}

// Identifier resolves the user associated with a request source.
type Identifier interface {
	Identify(ctx context.Context, remoteAddr string) (User, error)
}

// ErrUnauthenticated indicates that no browser user could be resolved.
var ErrUnauthenticated = errors.New("identity: unauthenticated")

// ErrForbidden indicates that the user is authenticated but lacks access.
var ErrForbidden = errors.New("identity: forbidden")

type contextKey struct{}

// WithUser returns a context carrying the authenticated user.
func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, contextKey{}, user)
}

// UserFrom returns the authenticated user from a context.
func UserFrom(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(contextKey{}).(User)
	return user, ok
}

// HasCapability reports whether the user has the named capability.
func (u User) HasCapability(capability string) bool {
	return slices.Contains(u.Capabilities, capability)
}

// RequireAdmin rejects users without the Warpgate admin capability.
func RequireAdmin(user User) error {
	if user.Email == "" {
		return ErrUnauthenticated
	}
	if !user.HasCapability(AdminCapability) {
		return ErrForbidden
	}
	return nil
}

// StaticIdentifier returns one configured user for local development.
type StaticIdentifier struct {
	// User is the user returned for every request.
	User User
}

// Identify returns the configured static user.
func (s StaticIdentifier) Identify(_ context.Context, _ string) (User, error) {
	if s.User.Email == "" {
		return User{}, ErrUnauthenticated
	}
	return s.User, nil
}
