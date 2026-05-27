package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	pkgdeploy "github.com/pangobit/warpgate/pkg/deploy"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/usecase"
)

func TestMapRuntimeStatusIncludesClusterNodes(t *testing.T) {
	repo := &config.RepoConfig{
		Cluster: &config.ClusterConfig{
			Nodes: []config.NodeConfig{
				{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.64.0.1"},
				{ID: "node-2", Host: "10.0.0.2", PrivateIP: "100.64.0.2"},
			},
		},
	}
	result := &pkgdeploy.ClusterStatusResult{
		NodeReachable: map[string]bool{"node-1": true},
		Apps: []pkgdeploy.AppNodeStatus{{
			App:     "api",
			NodeID:  "node-1",
			Version: "rel-1",
			Slot:    "blue",
			State:   "healthy",
			Services: []pkgdeploy.ContainerStatus{{
				Service: "api",
				Name:    "api-1",
				State:   "healthy",
			}},
		}},
	}

	status := mapRuntimeStatus(repo, result)

	if len(status.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(status.Nodes))
	}
	if !status.Nodes[0].Reachable {
		t.Fatalf("node-1 should be reachable")
	}
	if status.Nodes[1].Reachable {
		t.Fatalf("node-2 should not be reachable")
	}
	if len(status.Apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(status.Apps))
	}
	app := status.Apps[0]
	if app.App != "api" || app.NodeID != "node-1" || app.State != "healthy" {
		t.Fatalf("mapped app = %+v", app)
	}
	if len(app.Services) != 1 || app.Services[0].Name != "api-1" {
		t.Fatalf("mapped services = %+v", app.Services)
	}
}

func TestMapConfigNodes(t *testing.T) {
	repo := &config.RepoConfig{
		Cluster: &config.ClusterConfig{
			Nodes: []config.NodeConfig{{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.64.0.1"}},
		},
	}

	nodes := mapConfigNodes(repo)

	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if nodes[0].ID != "node-1" || nodes[0].Host != "10.0.0.1" {
		t.Fatalf("node = %+v", nodes[0])
	}
}

func TestRepoFromRuntimeConfigUsesSyncedCluster(t *testing.T) {
	repo, err := repoFromRuntimeConfig(usecase.RuntimeConfigInput{
		Cluster: usecaseClusterSnapshot("100.95.30.69"),
		Apps: []configrepo.AppSnapshot{{
			Name:    "api",
			RawYAML: "kind: warpgate/app\nrelease:\n  services:\n    api:\n      image: ghcr.io/acme/api\n",
		}},
	})
	if err != nil {
		t.Fatalf("repoFromRuntimeConfig() error = %v", err)
	}
	node := repo.Cluster.GetNode("node-1")
	if node == nil {
		t.Fatalf("node-1 not found")
	}
	if node.PrivateIP != "100.95.30.69" {
		t.Fatalf("private ip = %q", node.PrivateIP)
	}
}

func TestSyncedRepoForReleaseWritesReleaseInputs(t *testing.T) {
	repo, cleanup, err := syncedRepoForRelease(usecase.DeployReleaseInput{
		App:          "api",
		ReleaseID:    "rel-1",
		ManifestJSON: `{"id":"rel-1","app":"api"}`,
		ReleaseManifests: []usecase.ReleaseManifestInput{{
			ID:           "old-release",
			ManifestJSON: `{"id":"old-release","app":"api"}`,
		}},
		Config: usecase.RuntimeConfigInput{
			Cluster: usecaseClusterSnapshot("100.95.30.69"),
			Apps: []configrepo.AppSnapshot{{
				Name:        "api",
				RawYAML:     "kind: warpgate/app\nrelease:\n  services:\n    api:\n      image: ghcr.io/acme/api\n",
				ComposeYAML: "services:\n  api:\n    image: ghcr.io/acme/api\n",
			}},
		},
	})
	if err != nil {
		t.Fatalf("syncedRepoForRelease() error = %v", err)
	}
	defer cleanup()

	if got := repo.Cluster.GetNode("node-1").PrivateIP; got != "100.95.30.69" {
		t.Fatalf("private ip = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo.AppReleasesDir("api"), "rel-1.json")); err != nil {
		t.Fatalf("release manifest missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.AppReleasesDir("api"), "old-release.json")); err != nil {
		t.Fatalf("old release manifest missing: %v", err)
	}
	if _, err := os.Stat(repo.AppComposePath("api")); err != nil {
		t.Fatalf("compose file missing: %v", err)
	}
}

func TestSyncedRepoForReleaseUsesResolvedSourceCompose(t *testing.T) {
	repo, cleanup, err := syncedRepoForRelease(usecase.DeployReleaseInput{
		App:          "api",
		ReleaseID:    "rel-1",
		ManifestJSON: `{"id":"rel-1","app":"api"}`,
		Config: usecase.RuntimeConfigInput{
			Cluster: usecaseClusterSnapshot("100.95.30.69"),
			Apps: []configrepo.AppSnapshot{{
				Name: "api",
				RawYAML: `kind: warpgate/app
compose_ref: master
source:
  repo: github.com/pangobit/brighter
  compose_path: deploy/compose.yml
release:
  services:
    api:
      image: ghcr.io/acme/api
`,
				ComposeYAML: "services:\n  api:\n    image: ghcr.io/acme/api\n",
			}},
		},
	})
	if err != nil {
		t.Fatalf("syncedRepoForRelease() error = %v", err)
	}
	defer cleanup()

	app := repo.GetApp("api")
	if app == nil {
		t.Fatalf("app missing")
	}
	if app.Source != nil {
		t.Fatalf("source should be resolved before deployment")
	}
	if _, err := os.Stat(repo.AppComposePath("api")); err != nil {
		t.Fatalf("compose file missing: %v", err)
	}
}

func TestSyncedRepoForReleaseRejectsUnresolvedSourceCompose(t *testing.T) {
	_, cleanup, err := syncedRepoForRelease(usecase.DeployReleaseInput{
		App:          "api",
		ReleaseID:    "rel-1",
		ManifestJSON: `{"id":"rel-1","app":"api"}`,
		Config: usecase.RuntimeConfigInput{
			Cluster: usecaseClusterSnapshot("100.95.30.69"),
			Apps: []configrepo.AppSnapshot{{
				Name: "api",
				RawYAML: `kind: warpgate/app
compose_ref: master
source:
  repo: github.com/pangobit/brighter
  compose_path: deploy/compose.yml
release:
  services:
    api:
      image: ghcr.io/acme/api
`,
			}},
		},
	})
	cleanup()
	if err == nil {
		t.Fatalf("syncedRepoForRelease() error = nil")
	}
}

func TestSyncedRepoForReleaseAllowsUnresolvedSourceComposeForOtherApps(t *testing.T) {
	repo, cleanup, err := syncedRepoForRelease(usecase.DeployReleaseInput{
		App:          "platform",
		ReleaseID:    "rel-platform",
		ManifestJSON: `{"id":"rel-platform","app":"platform"}`,
		Config: usecase.RuntimeConfigInput{
			Cluster: usecaseClusterSnapshot("100.95.30.69"),
			Apps: []configrepo.AppSnapshot{
				{
					Name:        "platform",
					RawYAML:     "kind: warpgate/app\nrelease:\n  services:\n    platform:\n      image: ghcr.io/acme/platform\n",
					ComposeYAML: "services:\n  platform:\n    image: ghcr.io/acme/platform\n",
				},
				{
					Name: "site",
					RawYAML: `kind: warpgate/app
compose_ref: master
source:
  repo: github.com/pangobit/brighter
  compose_path: deploy/compose.yml
release:
  services:
    site:
      image: ghcr.io/acme/site
`,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("syncedRepoForRelease() error = %v", err)
	}
	defer cleanup()

	if repo.GetApp("platform") == nil {
		t.Fatalf("platform app missing")
	}
	site := repo.GetApp("site")
	if site == nil || site.Source == nil {
		t.Fatalf("site source app missing: %+v", site)
	}
}

func TestMapAppRuntimeStatus(t *testing.T) {
	statuses := mapAppRuntimeStatus([]pkgdeploy.NodeStatus{{
		NodeID:        "node-1",
		State:         "running",
		Version:       "rel-1",
		Slot:          "green",
		Containers:    "api\tUp",
		ShadowVersion: "rel-2",
		ShadowState:   "healthy",
	}})

	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	status := statuses[0]
	if status.NodeID != "node-1" || status.Version != "rel-1" || status.ShadowVersion != "rel-2" {
		t.Fatalf("mapped status = %+v", status)
	}
}

func TestMapLogsResult(t *testing.T) {
	result := mapLogsResult(pkgdeploy.LogsResult{Output: "[api] ok\n"})

	if result.Output != "[api] ok\n" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestFindClusterPathUsesRepositorySubpath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "prod", "cluster.yml"))

	path, err := findClusterPath(root, "prod")
	if err != nil {
		t.Fatalf("findClusterPath() error = %v", err)
	}
	if path != filepath.Join(root, "prod", "cluster.yml") {
		t.Fatalf("path = %q", path)
	}
}

func TestFindClusterPathSearchesTwoDirectoriesDeep(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "examples", "infra-repo", "cluster.yml"))

	path, err := findClusterPath(root, "")
	if err != nil {
		t.Fatalf("findClusterPath() error = %v", err)
	}
	if path != filepath.Join(root, "examples", "infra-repo", "cluster.yml") {
		t.Fatalf("path = %q", path)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func usecaseClusterSnapshot(privateIP string) configrepo.ClusterSnapshot {
	return configrepo.ClusterSnapshot{
		Path: "prod/cluster.yml",
		RawYAML: `version: "2"
project: acme
nodes:
  - id: node-1
    host: 10.0.0.1
    private_ip: ` + privateIP + `
networking:
  private_network: example.ts.net
  dns:
    provider: cloudflare
    zone: example.com
  traefik:
    entry_points: [web]
registry:
  server: ghcr.io
`,
	}
}
