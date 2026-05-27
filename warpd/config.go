// Package warpd composes and runs the Warpgate local browser UI.
package warpd

import (
	"os"
	"path/filepath"
)

// DeployConfig holds deploy adapter boot settings.
type DeployConfig struct {
	// RepoPath is a local checkout used by the deploy adapter.
	RepoPath string
	// SSHKey is the deploy SSH private key path.
	SSHKey string
	// TailscaleSSH enables Tailscale SSH for deploys.
	TailscaleSSH bool
	// User is the deploy SSH user.
	User string
	// GitHubTokenEnvVar names the GitHub token env var used by deploy operations.
	GitHubTokenEnvVar string
}

// LocalUIConfig holds local browser UI configuration.
type LocalUIConfig struct {
	// HTTPAddr is the local web server listen address.
	HTTPAddr string
	// DBPath is the local UI database path.
	DBPath string
	// GitHubClientID is the GitHub App client ID used for device flow.
	GitHubClientID string
	// OpenBrowser opens the local UI in the default browser.
	OpenBrowser bool
	// Deploy holds deploy adapter settings.
	Deploy DeployConfig
}

// DefaultLocalUIConfig returns local browser UI defaults.
func DefaultLocalUIConfig() LocalUIConfig {
	return LocalUIConfig{
		HTTPAddr:    "127.0.0.1:0",
		DBPath:      defaultLocalDBPath(),
		OpenBrowser: true,
		Deploy: DeployConfig{
			RepoPath:          ".",
			TailscaleSSH:      true,
			GitHubTokenEnvVar: "GITHUB_TOKEN",
		},
	}
}

func defaultLocalDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "warpgate.db"
	}
	return filepath.Join(dir, "warpgate", "warpgate.db")
}
