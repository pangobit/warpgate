// Package tailscale implements Tailscale-backed identity resolution.
package tailscale

import (
	"context"
	"fmt"

	"github.com/pangobit/warpgate/warpd/internal/identity"
	"tailscale.com/client/local"
	"tailscale.com/tailcfg"
)

// Identifier resolves browser users through the local Tailscale daemon.
type Identifier struct {
	client *local.Client
}

// NewIdentifier creates a Tailscale identity resolver.
func NewIdentifier() *Identifier {
	return &Identifier{client: &local.Client{}}
}

// Identify returns the user associated with a remote address.
func (i *Identifier) Identify(ctx context.Context, remoteAddr string) (identity.User, error) {
	who, err := i.client.WhoIs(ctx, remoteAddr)
	if err != nil {
		return identity.User{}, fmt.Errorf("tailscale whois: %w", err)
	}
	if who.UserProfile == nil || who.UserProfile.LoginName == "" {
		return identity.User{}, identity.ErrUnauthenticated
	}
	user := identity.User{
		Email:       who.UserProfile.LoginName,
		DisplayName: who.UserProfile.DisplayName,
	}
	for capability := range who.CapMap {
		user.Capabilities = append(user.Capabilities, string(capability))
	}
	if who.Node != nil {
		for capability := range who.Node.CapMap {
			user.Capabilities = append(user.Capabilities, string(tailcfg.NodeCapability(capability)))
		}
	}
	return user, nil
}
