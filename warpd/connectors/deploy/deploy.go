// Package deploy adapts the existing CLI deploy engine for warpd.
package deploy

import (
	"context"
	"os"
	"path/filepath"

	"github.com/pangobit/warpgate/pkg/config"
	pkgdeploy "github.com/pangobit/warpgate/pkg/deploy"
	"github.com/pangobit/warpgate/warpd/usecase"
)

// Adapter deploys releases through pkg/deploy.
type Adapter struct {
	// RepoPath is the local infrastructure repository checkout.
	RepoPath string
	// SSHKey is the path to the SSH private key.
	SSHKey string
	// TailscaleSSH enables Tailscale SSH.
	TailscaleSSH bool
	// User is the SSH username.
	User string
	// GitHubTokenEnvVar names the env var containing a GitHub token.
	GitHubTokenEnvVar string
}

// DeployRelease deploys the app through the existing deployment engine.
func (a Adapter) DeployRelease(ctx context.Context, input usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	if err := ctx.Err(); err != nil {
		return usecase.DeployResult{}, err
	}
	repo, err := config.LoadRepo(filepath.Join(a.RepoPath, "cluster.yml"))
	if err != nil {
		return usecase.DeployResult{}, err
	}
	app := repo.GetApp(input.App)
	if app == nil {
		return usecase.DeployResult{}, os.ErrNotExist
	}
	targets := app.GetTargetNodes(repo.Cluster.Nodes)
	deployer := pkgdeploy.NewDeployer(repo, a.SSHKey)
	deployer.TailscaleSSH = a.TailscaleSSH
	deployer.User = a.User
	if a.GitHubTokenEnvVar != "" {
		deployer.GitHubToken = os.Getenv(a.GitHubTokenEnvVar)
	}
	if err := deployer.Deploy(input.App, ""); err != nil {
		return usecase.DeployResult{}, err
	}
	return usecase.DeployResult{Targets: targets}, nil
}
