package usecase_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/deployment"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/usecase"
)

func TestAttachRepositoryImportsExistingWarpgateRepo(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	service := usecase.NewService(store, github, fakeRegistry{}, fakeDeployer{})

	err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{
		Owner:  "acme",
		Repo:   "infra",
		Branch: "main",
	})
	if err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}

	apps, err := store.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(apps))
	}
	if apps[0].Name != "api" {
		t.Fatalf("app name = %q, want api", apps[0].Name)
	}
	cursor, err := store.ConfigCursor(ctx)
	if err != nil {
		t.Fatalf("ConfigCursor() error = %v", err)
	}
	if cursor.LastObservedCommit != "commit-1" {
		t.Fatalf("commit = %q, want commit-1", cursor.LastObservedCommit)
	}
}

func TestAttachRepositoryImportsRepositorySubpath(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	service := usecase.NewService(store, github, fakeRegistry{}, fakeDeployer{})

	err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{
		Owner:  "acme",
		Repo:   "infra",
		Branch: "main",
		Path:   "/prod/",
	})
	if err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}

	settings, ok, err := store.RepositorySettings(ctx)
	if err != nil || !ok {
		t.Fatalf("RepositorySettings() ok = %v error = %v", ok, err)
	}
	if settings.Path != "prod" {
		t.Fatalf("path = %q, want prod", settings.Path)
	}
	apps, err := store.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(apps))
	}
	if apps[0].Path != "prod/apps/api/app.yml" {
		t.Fatalf("app path = %q, want prod/apps/api/app.yml", apps[0].Path)
	}
}

func TestCommitReleaseRejectsMovedBranch(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	service := usecase.NewService(store, github, fakeRegistry{}, fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   appYAML("v2.0.0"),
		SHA:       "sha-moved",
		CommitSHA: "commit-2",
	}

	_, err := service.CommitRelease(ctx, adminUser(), "api", []release.DeployDataChange{{
		Service:  "api",
		ImageTag: "v2.0.0",
	}})
	if !errors.Is(err, usecase.ErrConflict) {
		t.Fatalf("CommitRelease() error = %v, want conflict", err)
	}
}

func TestCheckImagesRecordsDigestChanges(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	registry := fakeRegistry{digests: map[string]string{"ghcr.io/acme/api:v1.0.0": "sha256:first"}}
	service := usecase.NewService(store, github, registry, fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	if err := service.CheckImages(ctx, adminUser()); err != nil {
		t.Fatalf("CheckImages() first error = %v", err)
	}
	registry.digests["ghcr.io/acme/api:v1.0.0"] = "sha256:second"
	if err := service.CheckImages(ctx, adminUser()); err != nil {
		t.Fatalf("CheckImages() second error = %v", err)
	}
	cursors, err := store.ListImageCursors(ctx)
	if err != nil {
		t.Fatalf("ListImageCursors() error = %v", err)
	}
	if len(cursors) != 1 {
		t.Fatalf("cursors = %d, want 1", len(cursors))
	}
	if cursors[0].Status != imagewatch.StatusChanged {
		t.Fatalf("status = %q, want changed", cursors[0].Status)
	}
	if cursors[0].PreviousDigest != "sha256:first" || cursors[0].LastDigest != "sha256:second" {
		t.Fatalf("digests = %q -> %q", cursors[0].PreviousDigest, cursors[0].LastDigest)
	}
}

func TestDeployReleaseRecordsSuccessfulAttempt(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, fakeDeployer{targets: []string{"node-1"}})
	if err := store.CreateRelease(ctx, release.Record{
		ID:           "rel-1",
		App:          "api",
		ConfigCommit: "commit-1",
		Status:       release.StatusReady,
		ActorEmail:   adminUser().Email,
	}); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}

	record, err := service.DeployRelease(ctx, adminUser(), "rel-1")
	if err != nil {
		t.Fatalf("DeployRelease() error = %v", err)
	}
	if record.Status != deployment.StatusSucceeded {
		t.Fatalf("deployment status = %q, want succeeded", record.Status)
	}
	releaseRecord, ok, err := store.Release(ctx, "rel-1")
	if err != nil || !ok {
		t.Fatalf("Release() ok = %v error = %v", ok, err)
	}
	if releaseRecord.Status != release.StatusDeployed {
		t.Fatalf("release status = %q, want deployed", releaseRecord.Status)
	}
}

type fakeGitHub struct {
	head  string
	files map[string]usecase.GitHubFile
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{
		head: "commit-1",
		files: map[string]usecase.GitHubFile{
			"cluster.yml": {
				Path:      "cluster.yml",
				Content:   clusterYAML(),
				SHA:       "cluster-sha",
				CommitSHA: "commit-1",
			},
			"apps/api/app.yml": {
				Path:      "apps/api/app.yml",
				Content:   appYAML("v1.0.0"),
				SHA:       "app-sha",
				CommitSHA: "commit-1",
			},
			"apps/api/compose.yml": {
				Path:      "apps/api/compose.yml",
				Content:   "services:\n  api:\n    image: ghcr.io/acme/api\n",
				SHA:       "compose-sha",
				CommitSHA: "commit-1",
			},
			"prod/cluster.yml": {
				Path:      "prod/cluster.yml",
				Content:   clusterYAML(),
				SHA:       "prod-cluster-sha",
				CommitSHA: "commit-1",
			},
			"prod/apps/api/app.yml": {
				Path:      "prod/apps/api/app.yml",
				Content:   appYAML("v1.0.0"),
				SHA:       "prod-app-sha",
				CommitSHA: "commit-1",
			},
			"prod/apps/api/compose.yml": {
				Path:      "prod/apps/api/compose.yml",
				Content:   "services:\n  api:\n    image: ghcr.io/acme/api\n",
				SHA:       "prod-compose-sha",
				CommitSHA: "commit-1",
			},
		},
	}
}

// BranchHead returns the fake branch head.
func (f *fakeGitHub) BranchHead(context.Context, configrepo.RepositorySettings) (string, error) {
	return f.head, nil
}

// ReadFile returns a fake repository file by path.
func (f *fakeGitHub) ReadFile(_ context.Context, _ configrepo.RepositorySettings, path string, _ string) (usecase.GitHubFile, error) {
	file, ok := f.files[path]
	if !ok {
		return usecase.GitHubFile{}, errors.New("not found")
	}
	return file, nil
}

// ListAppConfigFiles returns fake app config files under the configured root.
func (f *fakeGitHub) ListAppConfigFiles(_ context.Context, settings configrepo.RepositorySettings, _ string) ([]usecase.GitHubFile, error) {
	prefix := "apps/"
	root := strings.Trim(strings.TrimSpace(settings.Path), "/")
	if root != "" {
		prefix = root + "/apps/"
	}
	var files []usecase.GitHubFile
	for name, file := range f.files {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, "/app.yml") {
			files = append(files, file)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// WriteFile updates a fake repository file.
func (f *fakeGitHub) WriteFile(_ context.Context, input usecase.WriteFileInput) (usecase.GitHubFile, error) {
	file := f.files[input.Path]
	if file.SHA != input.ExpectedSHA {
		return usecase.GitHubFile{}, usecase.ErrConflict
	}
	file.Content = input.Content
	file.SHA = "app-sha-next"
	file.CommitSHA = "commit-next"
	f.files[input.Path] = file
	return file, nil
}

type fakeRegistry struct {
	digests map[string]string
}

// ResolveDigest returns a fake image digest.
func (f fakeRegistry) ResolveDigest(_ context.Context, image string, tag string) (string, error) {
	digest, ok := f.digests[image+":"+tag]
	if !ok {
		return "", errors.New("digest not found")
	}
	return digest, nil
}

type fakeDeployer struct {
	targets []string
	err     error
}

// DeployRelease returns a fake deployment result.
func (f fakeDeployer) DeployRelease(context.Context, usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	return usecase.DeployResult{Targets: f.targets}, f.err
}

func adminUser() identity.User {
	return identity.User{
		Email:        "admin@example.com",
		DisplayName:  "Admin",
		Capabilities: []string{identity.AdminCapability},
	}
}

func clusterYAML() string {
	return `version: "2"
project: acme
nodes:
  - id: node-1
    host: 10.0.0.1
networking:
  private_network: example.ts.net
  dns:
    provider: cloudflare
    zone: example.com
  traefik:
    entry_points: [web]
registry:
  server: ghcr.io
`
}

func appYAML(tag string) string {
	return `kind: warpgate/app
targets: [node-1]
release:
  services:
    api:
      image: ghcr.io/acme/api
      image_tag: ` + tag + `
      port: 8080
      expose:
        public:
          domains: [api.example.com]
`
}
