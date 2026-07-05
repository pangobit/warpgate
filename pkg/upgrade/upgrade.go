// Package upgrade installs a new Warpgate daemon binary from GitHub Releases.
package upgrade

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pangobit/warpgate/pkg/version"
)

// ReleaseFetcher resolves release metadata for an upgrade target.
type ReleaseFetcher interface {
	FetchRelease(ctx context.Context, version, goos, goarch string) (*Release, error)
}

// AssetDownloader downloads and verifies release assets.
type AssetDownloader interface {
	DownloadVerified(ctx context.Context, assetURL, checksumsURL, assetName string) ([]byte, error)
}

// BinaryInstaller installs a verified binary on disk.
type BinaryInstaller interface {
	Install(installPath string, data []byte) error
	RestoreBackup(installPath string) error
}

// ServiceController manages the daemon systemd unit.
type ServiceController interface {
	State(serviceName string) (exists bool, active bool, err error)
	Stop(serviceName string) error
	Start(serviceName string) error
}

// Upgrader replaces the installed daemon binary from GitHub Releases.
type Upgrader struct {
	// Releases resolves release metadata for the target version.
	Releases ReleaseFetcher
	// Downloads fetches and verifies release assets.
	Downloads AssetDownloader
	// Install writes the verified binary to disk.
	Install BinaryInstaller
	// Service controls the daemon systemd unit.
	Service ServiceController
	// Output receives upgrade progress messages.
	Output io.Writer
}

// Run performs the upgrade sequence.
func (u *Upgrader) Run(ctx context.Context, opts Options) error {
	if u.Output == nil {
		u.Output = os.Stdout
	}
	if u.Releases == nil {
		u.Releases = &GitHubReleaseClient{Owner: opts.RepoOwner, Name: opts.RepoName}
	}
	if u.Downloads == nil {
		u.Downloads = &HTTPDownloader{}
	}
	if u.Install == nil {
		u.Install = FileInstaller{}
	}
	if u.Service == nil {
		u.Service = SystemdServiceManager{}
	}

	installPath, err := resolveInstallPath(opts.InstallPath)
	if err != nil {
		return err
	}
	serviceName := opts.ServiceName
	if serviceName == "" {
		serviceName = "warpgate"
	}
	targetVersion := opts.Version
	if targetVersion == "" {
		targetVersion = "latest"
	}

	release, err := u.Releases.FetchRelease(ctx, targetVersion, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	currentVersion := version.Current()
	if currentVersion == release.Tag {
		fmt.Fprintf(u.Output, "Already at %s\n", release.Tag)
		return nil
	}

	serviceExists, serviceActive, err := u.Service.State(serviceName)
	if err != nil {
		return err
	}

	if opts.DryRun {
		fmt.Fprintf(u.Output, "Current: %s\n", currentVersion)
		fmt.Fprintf(u.Output, "Target: %s (%s)\n", release.Tag, release.AssetName)
		fmt.Fprintf(u.Output, "Install path: %s\n", installPath)
		if serviceExists {
			if opts.NoRestart {
				fmt.Fprintf(u.Output, "Would leave %s.service unchanged\n", serviceName)
			} else if serviceActive {
				fmt.Fprintf(u.Output, "Would stop and start %s.service\n", serviceName)
			} else {
				fmt.Fprintf(u.Output, "Would start %s.service\n", serviceName)
			}
		} else {
			fmt.Fprintf(u.Output, "No %s.service unit found\n", serviceName)
		}
		return nil
	}

	fmt.Fprintf(u.Output, "Current: %s\n", currentVersion)
	fmt.Fprintf(u.Output, "Downloading %s (%s)...\n", release.Tag, release.AssetName)
	data, err := u.Downloads.DownloadVerified(ctx, release.AssetURL, release.ChecksumsURL, release.AssetName)
	if err != nil {
		return err
	}

	shouldRestart := serviceExists && serviceActive && !opts.NoRestart
	if shouldRestart {
		fmt.Fprintf(u.Output, "Stopping %s.service...\n", serviceName)
		if err := u.Service.Stop(serviceName); err != nil {
			return err
		}
	}

	if err := u.Install.Install(installPath, data); err != nil {
		return err
	}
	fmt.Fprintf(u.Output, "Installed %s\n", installPath)

	if serviceExists && !opts.NoRestart {
		fmt.Fprintf(u.Output, "Starting %s.service...\n", serviceName)
		if err := u.Service.Start(serviceName); err != nil {
			restoreErr := u.Install.RestoreBackup(installPath)
			startErr := u.Service.Start(serviceName)
			if restoreErr != nil {
				return fmt.Errorf("start service failed: %w; restore backup failed: %v", err, restoreErr)
			}
			if startErr != nil {
				return fmt.Errorf("start service failed: %w; restored backup but restart failed: %v", err, startErr)
			}
			return fmt.Errorf("start service failed: %w; restored previous binary", err)
		}
	}

	fmt.Fprintf(u.Output, "Upgraded: %s → %s\n", currentVersion, release.Tag)
	return nil
}

// DefaultInstallPath returns the absolute path of the running binary.
func DefaultInstallPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

func resolveInstallPath(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return DefaultInstallPath()
}
