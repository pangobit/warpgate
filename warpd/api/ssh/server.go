// Package ssh serves the Warpgate operator TUI over SSH.
package ssh

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	wishtea "charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/usecase"
)

// Config holds operator TUI server settings.
type Config struct {
	// Addr is the SSH listen address.
	Addr string
	// HostKeyPath is the daemon host key path, generated when missing.
	HostKeyPath string
	// Refresh schedules an immediate daemon reconcile.
	Refresh func()
}

// NewServer creates the SSH server hosting the operator TUI.
// Connections are trusted at the network layer: the listener must only be
// reachable over the tailnet, where Tailscale ACLs control access.
func NewServer(service *usecase.Service, cfg Config) (*ssh.Server, error) {
	return wish.NewServer(
		wish.WithAddress(cfg.Addr),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		wish.WithMiddleware(
			wishtea.Middleware(func(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
				return newModel(service, sessionActor(sess), cfg.Refresh), wishtea.MakeOptions(sess)
			}),
			activeterm.Middleware(),
		),
	)
}

// RunLocal runs the operator TUI directly in the current terminal.
func RunLocal(service *usecase.Service, actor identity.User, refresh func()) error {
	program := tea.NewProgram(newModel(service, actor, refresh))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run operator TUI: %w", err)
	}
	return nil
}

func sessionActor(sess ssh.Session) identity.User {
	name := sess.User()
	if name == "" {
		name = "operator"
	}
	return identity.User{
		Email:        name + "@ssh",
		DisplayName:  name,
		Capabilities: []string{identity.AdminCapability},
	}
}
