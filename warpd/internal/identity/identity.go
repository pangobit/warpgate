// Package identity defines Warpgate web identity and authorization concepts.
package identity

import (
	"context"
	"errors"
	"slices"
	"time"
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

// GitHubAuthStatus describes the local GitHub authorization state.
type GitHubAuthStatus struct {
	// Configured reports whether a GitHub App client ID is available.
	Configured bool
	// ClientID is the configured GitHub App client ID.
	ClientID string
	// Authenticated reports whether Warpgate has a usable GitHub token.
	Authenticated bool
	// Login is the authenticated GitHub login.
	Login string
	// DisplayName is the authenticated GitHub display name.
	DisplayName string
	// UserCode is the pending device-flow code.
	UserCode string
	// VerificationURI is the GitHub device-flow authorization URL.
	VerificationURI string
	// Error is the latest authorization error.
	Error string
}

// GitHubSession is a persisted GitHub App user authorization.
type GitHubSession struct {
	// AccessToken is the GitHub user access token.
	AccessToken string
	// AccessTokenExpiresAt is when the access token expires.
	AccessTokenExpiresAt time.Time
	// RefreshToken is the GitHub user refresh token.
	RefreshToken string
	// RefreshTokenExpiresAt is when the refresh token expires.
	RefreshTokenExpiresAt time.Time
	// TokenType is the token type returned by GitHub.
	TokenType string
	// User is the GitHub user associated with the token.
	User User
	// UpdatedAt is when the token record was last changed.
	UpdatedAt time.Time
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
