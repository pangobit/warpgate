// Package warpd composes and runs the Warpgate daemon.
package warpd

import (
	"os"
	"path/filepath"
	"strings"
)

// DeployConfig holds deploy adapter boot settings.
type DeployConfig struct {
	// SSHKey is the deploy SSH private key path.
	SSHKey string
	// TailscaleSSH enables Tailscale SSH for deploys.
	TailscaleSSH bool
	// User is the deploy SSH user.
	User string
}

func defaultLocalDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "warpgate.db"
	}
	return filepath.Join(dir, "warpgate", "warpgate.db")
}

// RepoConfig identifies the desired-state repository the daemon watches.
type RepoConfig struct {
	// Owner is the GitHub repository owner.
	Owner string
	// Repo is the GitHub repository name.
	Repo string
	// Branch is the branch the daemon reads from and writes bumps to.
	Branch string
	// Path is the optional repository subdirectory containing cluster.yml and apps/.
	Path string
}

// ServeConfig holds Warpgate daemon configuration.
type ServeConfig struct {
	// HTTPAddr is the CI HTTP API listen address.
	HTTPAddr string
	// SSHAddr is the operator TUI SSH listen address.
	SSHAddr string
	// HostKeyPath is the daemon SSH host key path, generated when missing.
	HostKeyPath string
	// DBPath is the daemon database path.
	DBPath string
	// RegistryToken authenticates GHCR reads (classic PAT with read:packages).
	// GHCR does not accept GitHub App installation tokens, so without this
	// only public images can be watched.
	RegistryToken string
	// Repository is the desired-state repository to watch.
	Repository RepoConfig
	// Deploy holds deploy adapter settings.
	Deploy DeployConfig
}

// DefaultServeConfig returns daemon defaults, reading repository settings from the environment.
func DefaultServeConfig() ServeConfig {
	cfg := ServeConfig{
		HTTPAddr:      envOr("WARPGATE_HTTP_ADDR", "127.0.0.1:7411"),
		SSHAddr:       envOr("WARPGATE_SSH_ADDR", "127.0.0.1:7422"),
		HostKeyPath:   envOr("WARPGATE_HOST_KEY", defaultHostKeyPath()),
		DBPath:        envOr("WARPGATE_DB_PATH", defaultLocalDBPath()),
		RegistryToken: os.Getenv("WARPGATE_REGISTRY_TOKEN"),
		Repository: RepoConfig{
			Branch: envOr("WARPGATE_REPO_BRANCH", "master"),
			Path:   os.Getenv("WARPGATE_REPO_PATH"),
		},
		Deploy: DeployConfig{
			TailscaleSSH: true,
		},
	}
	if repo := os.Getenv("WARPGATE_REPO"); repo != "" {
		if owner, name, ok := strings.Cut(repo, "/"); ok {
			cfg.Repository.Owner = owner
			cfg.Repository.Repo = name
		}
	}
	return cfg
}

func defaultHostKeyPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "warpgate_host_key"
	}
	return filepath.Join(dir, "warpgate", "host_key")
}

func envOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
