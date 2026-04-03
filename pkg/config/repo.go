package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RepoConfig represents a warpgate infra repo with a cluster config and per-app configs.
type RepoConfig struct {
	// Root is the absolute path to the repo root (directory containing cluster.yml).
	Root string
	// Cluster is the cluster-level configuration.
	Cluster *ClusterConfig
	// Apps is the list of discovered app configurations.
	Apps []*AppConfig
}

// LoadRepo loads a complete repo config from the given cluster.yml path.
// It discovers apps by scanning the apps/ directory relative to cluster.yml.
func LoadRepo(clusterPath string) (*RepoConfig, error) {
	absPath, err := filepath.Abs(clusterPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	cluster, err := LoadClusterConfig(absPath)
	if err != nil {
		return nil, err
	}

	if err := cluster.Validate(); err != nil {
		return nil, fmt.Errorf("invalid cluster config: %w", err)
	}

	root := filepath.Dir(absPath)
	apps, err := DiscoverApps(root)
	if err != nil {
		return nil, err
	}

	return &RepoConfig{
		Root:    root,
		Cluster: cluster,
		Apps:    apps,
	}, nil
}

// DiscoverApps finds and loads all app.yml files under the apps/ directory.
func DiscoverApps(repoRoot string) ([]*AppConfig, error) {
	appsDir := filepath.Join(repoRoot, "apps")

	entries, err := os.ReadDir(appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}

	var apps []*AppConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appDir := filepath.Join(appsDir, entry.Name())
		appYml := filepath.Join(appDir, "app.yml")
		if _, err := os.Stat(appYml); os.IsNotExist(err) {
			continue
		}

		app, err := LoadAppConfig(appDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load app %s: %w", entry.Name(), err)
		}

		app.Name = entry.Name()

		if err := ValidateApp(app); err != nil {
			return nil, err
		}

		apps = append(apps, app)
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Name < apps[j].Name
	})

	return apps, nil
}

// GetApp returns the app with the given name, or nil if not found.
func (r *RepoConfig) GetApp(name string) *AppConfig {
	for _, app := range r.Apps {
		if app.Name == name {
			return app
		}
	}
	return nil
}

// GetAppsForNode returns all apps that target the given node ID.
func (r *RepoConfig) GetAppsForNode(nodeID string) []*AppConfig {
	var result []*AppConfig
	for _, app := range r.Apps {
		for _, target := range app.GetTargetNodes(r.Cluster.Nodes) {
			if target == nodeID {
				result = append(result, app)
				break
			}
		}
	}
	return result
}

// InternalHosts returns all internal hostnames configured across all apps and sidecars.
func (r *RepoConfig) InternalHosts() []string {
	var hosts []string
	for _, app := range r.Apps {
		if ie := app.EffectiveExpose().Internal; ie != nil {
			hosts = append(hosts, ie.Hostname)
		}
		for _, sidecar := range app.Sidecars {
			if ie := sidecar.EffectiveExpose().Internal; ie != nil {
				hosts = append(hosts, ie.Hostname)
			}
		}
	}
	return hosts
}

// AppDir returns the absolute path to an app's directory in the repo.
func (r *RepoConfig) AppDir(appName string) string {
	return filepath.Join(r.Root, "apps", appName)
}

// AppComposePath returns the absolute path to an app's compose.yml.
func (r *RepoConfig) AppComposePath(appName string) string {
	return filepath.Join(r.Root, "apps", appName, "compose.yml")
}
