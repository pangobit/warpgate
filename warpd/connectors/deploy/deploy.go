// Package deploy adapts the existing CLI deploy engine for warpd.
package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pangobit/warpgate/pkg/config"
	pkgdeploy "github.com/pangobit/warpgate/pkg/deploy"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/usecase"
	"gopkg.in/yaml.v3"
)

// TokenProvider supplies GitHub tokens for deploy-time registry access.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// Adapter deploys releases through pkg/deploy.
type Adapter struct {
	// SSHKey is the path to the SSH private key.
	SSHKey string
	// TailscaleSSH enables Tailscale SSH.
	TailscaleSSH bool
	// User is the SSH username.
	User string
	// GitHubTokenEnvVar names the env var containing a GitHub token.
	GitHubTokenEnvVar string
	// TokenSource mints GitHub tokens on demand; preferred over GitHubTokenEnvVar.
	TokenSource TokenProvider
}

// DeployRelease deploys the app through the existing deployment engine.
func (a Adapter) DeployRelease(ctx context.Context, input usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	if err := ctx.Err(); err != nil {
		return usecase.DeployResult{}, err
	}
	repo, cleanup, err := syncedRepoForRelease(input)
	if err != nil {
		return usecase.DeployResult{}, err
	}
	defer cleanup()
	app := repo.GetApp(input.App)
	if app == nil {
		return usecase.DeployResult{}, os.ErrNotExist
	}
	targets := app.GetTargetNodes(repo.Cluster.Nodes)
	deployer, err := a.newDeployer(ctx, repo)
	if err != nil {
		return usecase.DeployResult{}, err
	}
	if err := deployer.DeployRelease(input.App, input.ReleaseID); err != nil {
		return usecase.DeployResult{}, err
	}
	return usecase.DeployResult{Targets: targets}, nil
}

// ConfigNodes reads cluster nodes from synced desired-state config.
func (a Adapter) ConfigNodes(ctx context.Context, input usecase.RuntimeConfigInput) ([]usecase.ConfigNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := repoFromRuntimeConfig(input)
	if err != nil {
		return nil, err
	}
	return mapConfigNodes(repo), nil
}

// RuntimeStatus queries live runtime state through the existing deploy engine.
func (a Adapter) RuntimeStatus(ctx context.Context, input usecase.RuntimeConfigInput) (usecase.RuntimeStatus, error) {
	if err := ctx.Err(); err != nil {
		return usecase.RuntimeStatus{}, err
	}
	repo, err := repoFromRuntimeConfig(input)
	if err != nil {
		return usecase.RuntimeStatus{}, err
	}
	deployer, err := a.newDeployer(ctx, repo)
	if err != nil {
		return usecase.RuntimeStatus{}, err
	}
	result, err := deployer.ClusterStatus()
	if err != nil {
		return usecase.RuntimeStatus{}, err
	}
	return mapRuntimeStatus(repo, result), nil
}

// AppRuntimeStatus queries live runtime state for one app.
func (a Adapter) AppRuntimeStatus(ctx context.Context, input usecase.RuntimeConfigInput, app string) ([]usecase.RuntimeNodeStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := repoFromRuntimeConfig(input)
	if err != nil {
		return nil, err
	}
	deployer, err := a.newDeployer(ctx, repo)
	if err != nil {
		return nil, err
	}
	result, err := deployer.Status(app)
	if err != nil {
		return nil, err
	}
	return mapAppRuntimeStatus(result), nil
}

// Logs fetches recent live logs through the existing deploy engine.
func (a Adapter) Logs(ctx context.Context, runtimeConfig usecase.RuntimeConfigInput, input usecase.LogsInput) (usecase.LogsResult, error) {
	if err := ctx.Err(); err != nil {
		return usecase.LogsResult{}, err
	}
	repo, err := repoFromRuntimeConfig(runtimeConfig)
	if err != nil {
		return usecase.LogsResult{}, err
	}
	deployer, err := a.newDeployer(ctx, repo)
	if err != nil {
		return usecase.LogsResult{}, err
	}
	result, err := deployer.FetchLogs(pkgdeploy.LogsOptions{
		NodeID: input.NodeID,
		App:    input.App,
		Tail:   input.Tail,
		Grep:   input.Grep,
	})
	if err != nil {
		return usecase.LogsResult{}, err
	}
	return mapLogsResult(result), nil
}

func (a Adapter) newDeployer(ctx context.Context, repo *config.RepoConfig) (*pkgdeploy.Deployer, error) {
	deployer := pkgdeploy.NewDeployer(repo, a.SSHKey)
	deployer.TailscaleSSH = a.TailscaleSSH
	deployer.User = a.User
	token, err := a.githubToken(ctx)
	if err != nil {
		return nil, err
	}
	deployer.GitHubToken = token
	return deployer, nil
}

func (a Adapter) githubToken(ctx context.Context) (string, error) {
	if a.TokenSource != nil {
		return a.TokenSource.Token(ctx)
	}
	if a.GitHubTokenEnvVar != "" {
		return os.Getenv(a.GitHubTokenEnvVar), nil
	}
	return "", nil
}

func repoFromRuntimeConfig(input usecase.RuntimeConfigInput) (*config.RepoConfig, error) {
	var cluster config.ClusterConfig
	if err := yaml.Unmarshal([]byte(input.Cluster.RawYAML), &cluster); err != nil {
		return nil, fmt.Errorf("parse synced cluster.yml: %w", err)
	}
	if err := cluster.Validate(); err != nil {
		return nil, fmt.Errorf("invalid synced cluster.yml: %w", err)
	}
	apps := make([]*config.AppConfig, 0, len(input.Apps))
	for _, snapshot := range input.Apps {
		var app config.AppConfig
		if err := yaml.Unmarshal([]byte(snapshot.RawYAML), &app); err != nil {
			return nil, fmt.Errorf("parse synced app %s: %w", snapshot.Name, err)
		}
		app.Name = snapshot.Name
		if err := config.ValidateApp(&app); err != nil {
			return nil, err
		}
		apps = append(apps, &app)
	}
	return &config.RepoConfig{
		Cluster: &cluster,
		Apps:    apps,
	}, nil
}

func syncedRepoForRelease(input usecase.DeployReleaseInput) (*config.RepoConfig, func(), error) {
	root, err := os.MkdirTemp("", "warpgate-release-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create synced repo tempdir: %w", err)
	}
	cleanup := func() {
		if err := os.RemoveAll(root); err != nil {
			return
		}
	}
	if err := writeSyncedRepo(root, input.Config, input.App, input.ReleaseID, input.ManifestJSON, input.ReleaseManifests); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	repo, err := config.LoadRepo(filepath.Join(root, "cluster.yml"))
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return repo, cleanup, nil
}

func writeSyncedRepo(root string, input usecase.RuntimeConfigInput, releaseApp string, releaseID string, manifestJSON string, releaseManifests []usecase.ReleaseManifestInput) error {
	if err := writeFile(filepath.Join(root, "cluster.yml"), input.Cluster.RawYAML); err != nil {
		return err
	}
	for _, snapshot := range input.Apps {
		appDir := filepath.Join(root, "apps", snapshot.Name)
		appYAML := snapshot.RawYAML
		if snapshot.Name == releaseApp {
			resolvedYAML, err := deployAppYAML(snapshot)
			if err != nil {
				return err
			}
			appYAML = resolvedYAML
		}
		if err := writeFile(filepath.Join(appDir, "app.yml"), appYAML); err != nil {
			return err
		}
		if snapshot.ComposeYAML != "" {
			if err := writeFile(filepath.Join(appDir, "compose.yml"), snapshot.ComposeYAML); err != nil {
				return err
			}
		}
		for name, content := range snapshot.ExtraFiles {
			if err := writeFile(filepath.Join(appDir, name), content); err != nil {
				return err
			}
		}
		if snapshot.Name == releaseApp {
			if err := writeReleaseManifests(appDir, releaseID, manifestJSON, releaseManifests); err != nil {
				return err
			}
		}
	}
	return nil
}

func deployAppYAML(snapshot configrepo.AppSnapshot) (string, error) {
	var app config.AppConfig
	if err := yaml.Unmarshal([]byte(snapshot.RawYAML), &app); err != nil {
		return "", fmt.Errorf("parse synced app.yml: %w", err)
	}
	if app.Source == nil {
		return snapshot.RawYAML, nil
	}
	if snapshot.ComposeYAML == "" {
		return "", fmt.Errorf("app %s: source compose has not been synced", snapshot.Name)
	}
	app.Source = nil
	data, err := yaml.Marshal(&app)
	if err != nil {
		return "", fmt.Errorf("marshal deploy app.yml: %w", err)
	}
	return string(data), nil
}

func writeReleaseManifest(appDir string, releaseID string, manifestJSON string) error {
	if err := writeFile(filepath.Join(appDir, "releases", "latest.json"), manifestJSON); err != nil {
		return err
	}
	var manifest struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return fmt.Errorf("parse release manifest id: %w", err)
	}
	if manifest.ID == "" {
		manifest.ID = releaseID
	}
	if manifest.ID == "" {
		return nil
	}
	return writeFile(filepath.Join(appDir, "releases", manifest.ID+".json"), manifestJSON)
}

func writeReleaseManifests(appDir string, releaseID string, manifestJSON string, releaseManifests []usecase.ReleaseManifestInput) error {
	for _, manifest := range releaseManifests {
		if manifest.ManifestJSON == "" {
			continue
		}
		if err := writeReleaseManifest(appDir, manifest.ID, manifest.ManifestJSON); err != nil {
			return err
		}
	}
	return writeReleaseManifest(appDir, releaseID, manifestJSON)
}

func writeFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create %s directory: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func mapRuntimeStatus(repo *config.RepoConfig, result *pkgdeploy.ClusterStatusResult) usecase.RuntimeStatus {
	status := usecase.RuntimeStatus{
		Nodes: make([]usecase.RuntimeNode, 0, len(repo.Cluster.Nodes)),
		Apps:  make([]usecase.RuntimeAppStatus, 0, len(result.Apps)),
	}
	for _, node := range repo.Cluster.Nodes {
		status.Nodes = append(status.Nodes, usecase.RuntimeNode{
			ID:        node.ID,
			Host:      node.Host,
			PrivateIP: node.PrivateIP,
			Reachable: result.NodeReachable[node.ID],
		})
	}
	for _, app := range result.Apps {
		status.Apps = append(status.Apps, usecase.RuntimeAppStatus{
			App:           app.App,
			NodeID:        app.NodeID,
			Version:       app.Version,
			Slot:          app.Slot,
			State:         app.State,
			Services:      mapContainerStatus(app.Services),
			Error:         app.Error,
			ShadowVersion: app.ShadowVersion,
			ShadowState:   app.ShadowState,
		})
	}
	return status
}

func mapConfigNodes(repo *config.RepoConfig) []usecase.ConfigNode {
	nodes := make([]usecase.ConfigNode, 0, len(repo.Cluster.Nodes))
	for _, node := range repo.Cluster.Nodes {
		nodes = append(nodes, usecase.ConfigNode{
			ID:        node.ID,
			Host:      node.Host,
			PrivateIP: node.PrivateIP,
		})
	}
	return nodes
}

func mapAppRuntimeStatus(result []pkgdeploy.NodeStatus) []usecase.RuntimeNodeStatus {
	statuses := make([]usecase.RuntimeNodeStatus, 0, len(result))
	for _, status := range result {
		statuses = append(statuses, usecase.RuntimeNodeStatus{
			NodeID:        status.NodeID,
			State:         status.State,
			Version:       status.Version,
			Slot:          status.Slot,
			Containers:    status.Containers,
			Error:         status.Error,
			ShadowVersion: status.ShadowVersion,
			ShadowState:   status.ShadowState,
		})
	}
	return statuses
}

func mapContainerStatus(result []pkgdeploy.ContainerStatus) []usecase.RuntimeContainerStatus {
	statuses := make([]usecase.RuntimeContainerStatus, 0, len(result))
	for _, status := range result {
		statuses = append(statuses, usecase.RuntimeContainerStatus{
			Service: status.Service,
			Name:    status.Name,
			State:   status.State,
		})
	}
	return statuses
}

func mapLogsResult(result pkgdeploy.LogsResult) usecase.LogsResult {
	return usecase.LogsResult{
		Output:  result.Output,
		Message: result.Message,
	}
}
