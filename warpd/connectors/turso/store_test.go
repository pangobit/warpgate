package turso

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/identity"
)

func TestStorePersistsGitHubSession(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "warpgate.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	session := identity.GitHubSession{
		AccessToken:           "access-token",
		AccessTokenExpiresAt:  time.Unix(100, 0).UTC(),
		RefreshToken:          "refresh-token",
		RefreshTokenExpiresAt: time.Unix(200, 0).UTC(),
		TokenType:             "bearer",
		User: identity.User{
			Email:       "octo",
			DisplayName: "Octo User",
		},
		UpdatedAt: time.Unix(50, 0).UTC(),
	}
	if err := store.SaveGitHubSession(ctx, session); err != nil {
		t.Fatalf("SaveGitHubSession() error = %v", err)
	}
	got, ok, err := store.GitHubSession(ctx)
	if err != nil || !ok {
		t.Fatalf("GitHubSession() ok = %v error = %v", ok, err)
	}
	if got.AccessToken != session.AccessToken || got.RefreshToken != session.RefreshToken {
		t.Fatalf("tokens = %q / %q", got.AccessToken, got.RefreshToken)
	}
	if got.User.DisplayName != "Octo User" {
		t.Fatalf("display name = %q", got.User.DisplayName)
	}
	if err := store.DeleteGitHubSession(ctx); err != nil {
		t.Fatalf("DeleteGitHubSession() error = %v", err)
	}
	_, ok, err = store.GitHubSession(ctx)
	if err != nil || ok {
		t.Fatalf("GitHubSession() after delete ok = %v error = %v", ok, err)
	}
}
