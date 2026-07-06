package upgrade

// Options configures a daemon binary upgrade.
type Options struct {
	// Version is the target release tag or "latest".
	Version string
	// InstallPath is the destination binary path.
	InstallPath string
	// ServiceName is the systemd unit name without the .service suffix.
	ServiceName string
	// RepoOwner is the GitHub repository owner.
	RepoOwner string
	// RepoName is the GitHub repository name.
	RepoName string
	// NoRestart skips restarting the systemd service after install.
	NoRestart bool
	// DryRun prints the planned upgrade without modifying the host.
	DryRun bool
}
