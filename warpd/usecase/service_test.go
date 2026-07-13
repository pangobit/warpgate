package usecase_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/deployment"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/internal/stackstate"
	"github.com/pangobit/warpgate/warpd/usecase"
)

func TestAttachRepositoryImportsExistingWarpgateRepo(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})

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
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})

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
	if apps[0].ExtraFiles["vector.yaml"] != "sources: {}\n" {
		t.Fatalf("extra files = %#v, want vector.yaml synced", apps[0].ExtraFiles)
	}
}

func TestAttachRepositoryImportsRemoteSourceComposeWithGitHubClient(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   sourceAppYAML(),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	github.files["deploy/compose.yml"] = usecase.GitHubFile{
		Path:      "deploy/compose.yml",
		Content:   "services:\n  api:\n    image: ghcr.io/acme/api\n",
		SHA:       "source-compose-sha",
		CommitSHA: "source-commit",
	}
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})

	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}

	app, ok, err := store.App(ctx, "api")
	if err != nil || !ok {
		t.Fatalf("App() ok = %v error = %v", ok, err)
	}
	if !strings.Contains(app.ComposeYAML, "services:") {
		t.Fatalf("ComposeYAML = %q, want remote compose content", app.ComposeYAML)
	}
}

func TestCommitReleaseRejectsMovedBranch(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})
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

func TestCommitReleaseWritesAppAndReleaseFiles(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   multiServiceAppYAML("v1.0.0", "v1.0.0"),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}

	record, err := service.CommitRelease(ctx, adminUser(), "api", []release.DeployDataChange{
		{Service: "api", ImageTag: "v2.0.0"},
		{Service: "worker", ImageTag: "v2.0.0"},
	})
	if err != nil {
		t.Fatalf("CommitRelease() error = %v", err)
	}

	appFile := github.files["apps/api/app.yml"]
	if !strings.Contains(appFile.Content, "api:\n            image: ghcr.io/acme/api\n            image_tag: v2.0.0") {
		t.Fatalf("app.yml did not update api service:\n%s", appFile.Content)
	}
	if !strings.Contains(appFile.Content, "worker:\n            image: ghcr.io/acme/worker\n            image_tag: v2.0.0") {
		t.Fatalf("app.yml did not update worker service:\n%s", appFile.Content)
	}
	releasePath := "apps/api/releases/" + record.ID + ".json"
	if _, ok := github.files[releasePath]; !ok {
		t.Fatalf("expected release manifest file %s", releasePath)
	}
	latest := github.files["apps/api/releases/latest.json"]
	if latest.Content == "" {
		t.Fatalf("expected latest release manifest")
	}
	if latest.Content != record.ManifestJSON {
		t.Fatalf("latest.json content differs from release record")
	}
	if !strings.Contains(latest.Content, `"worker"`) || !strings.Contains(latest.Content, `"image_tag": "v2.0.0"`) {
		t.Fatalf("latest.json did not include updated services:\n%s", latest.Content)
	}
	if record.ConfigCommit != latest.CommitSHA {
		t.Fatalf("record commit = %q, want %q", record.ConfigCommit, latest.CommitSHA)
	}
}

func TestAppDetailReturnsAllReleaseServices(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   multiServiceAppYAML("v1.0.0", "v1.1.0"),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}

	detail, err := service.AppDetail(ctx, "api")
	if err != nil {
		t.Fatalf("AppDetail() error = %v", err)
	}
	if len(detail.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(detail.Services))
	}
	if detail.Services[0].Name != "api" || detail.Services[1].Name != "worker" {
		t.Fatalf("services = %+v", detail.Services)
	}
}

func TestListAppReleaseServicesReturnsSortedServices(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	if err := store.UpsertApp(ctx, configrepo.AppSnapshot{
		Name:    "api",
		RawYAML: multiServiceAppYAML("v1.0.0", "v1.1.0"),
	}); err != nil {
		t.Fatalf("UpsertApp() error = %v", err)
	}
	service := usecase.NewService(store, nil, nil, nil)

	entries, err := service.ListAppReleaseServices(ctx)
	if err != nil {
		t.Fatalf("ListAppReleaseServices() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Name != "api" {
		t.Fatalf("name = %q, want api", entries[0].Name)
	}
	if len(entries[0].Services) != 2 {
		t.Fatalf("services = %d, want 2", len(entries[0].Services))
	}
	if entries[0].Services[0].Name != "api" || entries[0].Services[1].Name != "worker" {
		t.Fatalf("services = %+v", entries[0].Services)
	}
	if got := usecase.ReleaseServiceImageRef(entries[0].Services[0]); got != "ghcr.io/acme/api:v1.0.0" {
		t.Fatalf("api image ref = %q, want ghcr.io/acme/api:v1.0.0", got)
	}
	if got := usecase.ReleaseServiceImageRef(entries[0].Services[1]); got != "ghcr.io/acme/worker:v1.1.0" {
		t.Fatalf("worker image ref = %q, want ghcr.io/acme/worker:v1.1.0", got)
	}
}

func TestListAppReleaseServicesContinuesAfterParseError(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	if err := store.UpsertApp(ctx, configrepo.AppSnapshot{Name: "broken", RawYAML: "not yaml"}); err != nil {
		t.Fatalf("UpsertApp(broken) error = %v", err)
	}
	if err := store.UpsertApp(ctx, configrepo.AppSnapshot{Name: "api", RawYAML: appYAML("v1.0.0")}); err != nil {
		t.Fatalf("UpsertApp(api) error = %v", err)
	}
	service := usecase.NewService(store, nil, nil, nil)

	entries, err := service.ListAppReleaseServices(ctx)
	if err != nil {
		t.Fatalf("ListAppReleaseServices() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Name != "api" {
		t.Fatalf("first entry = %q, want api", entries[0].Name)
	}
	if len(entries[0].Services) != 1 {
		t.Fatalf("api services = %d, want 1", len(entries[0].Services))
	}
	if entries[1].Name != "broken" {
		t.Fatalf("second entry = %q, want broken", entries[1].Name)
	}
	if entries[1].ParseError == "" {
		t.Fatal("expected parse error for broken app")
	}
}

func TestReleaseServiceImageRefPrefersTagOverDigest(t *testing.T) {
	ref := usecase.ReleaseServiceImageRef(usecase.AppReleaseService{
		Image:       "ghcr.io/acme/api",
		ImageTag:    "v1.0.0",
		ImageDigest: "sha256:abcdef1234567890abcdef1234567890abcdef12",
	})
	if ref != "ghcr.io/acme/api:v1.0.0" {
		t.Fatalf("image ref = %q, want ghcr.io/acme/api:v1.0.0", ref)
	}
}

func TestReleaseServiceImageRefUsesShortDigestWithoutTag(t *testing.T) {
	ref := usecase.ReleaseServiceImageRef(usecase.AppReleaseService{
		Image:       "ghcr.io/acme/api",
		ImageDigest: "sha256:abcdef1234567890abcdef1234567890abcdef12",
	})
	if ref != "ghcr.io/acme/api@sha256:abcdef123456" {
		t.Fatalf("image ref = %q, want ghcr.io/acme/api@sha256:abcdef123456", ref)
	}
}

func TestResolveBaselineReleasesReturnsDeployedServices(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	manifestJSON := `{
  "id": "rel-api-1",
  "app": "api",
  "image_ref": "ghcr.io/acme/api:v1.2.0",
  "image_tag": "v1.2.0",
  "services": {
    "api": {
      "image_ref": "ghcr.io/acme/api:v1.2.0",
      "image_tag": "v1.2.0",
      "env_hash": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    },
    "worker": {
      "image_ref": "ghcr.io/acme/worker:v1.1.0",
      "image_tag": "v1.1.0",
      "env_hash": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    }
  }
}`
	if err := store.CreateRelease(ctx, release.Record{
		ID:           "rel-api-1",
		App:          "api",
		ConfigCommit: "abcdef1234567890",
		ManifestJSON: manifestJSON,
	}); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	service := usecase.NewService(store, nil, nil, nil)

	entries, err := service.ResolveBaselineReleases(ctx, map[string]string{"api": "rel-api-1"})
	if err != nil {
		t.Fatalf("ResolveBaselineReleases() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if got := usecase.BaselineReleaseLabel(entries[0]); got != "commit abcdef12 · v1.2.0" {
		t.Fatalf("label = %q, want commit abcdef12 · v1.2.0", got)
	}
	if len(entries[0].Services) != 2 {
		t.Fatalf("services = %d, want 2", len(entries[0].Services))
	}
	if entries[0].Services[0].ImageRef != "ghcr.io/acme/api:v1.2.0" {
		t.Fatalf("api image ref = %q", entries[0].Services[0].ImageRef)
	}
}

func TestResolveBaselineReleasesMarksMissingRelease(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	service := usecase.NewService(store, nil, nil, nil)

	entries, err := service.ResolveBaselineReleases(ctx, map[string]string{"api": "missing"})
	if err != nil {
		t.Fatalf("ResolveBaselineReleases() error = %v", err)
	}
	if len(entries) != 1 || !entries[0].ReleaseMissing {
		t.Fatalf("entries = %+v, want missing release", entries)
	}
}

func TestResolveBaselineReleasesContinuesAfterManifestError(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	if err := store.CreateRelease(ctx, release.Record{
		ID: "rel-broken", App: "broken", ConfigCommit: "commit-1", ManifestJSON: "not json",
	}); err != nil {
		t.Fatalf("CreateRelease(broken) error = %v", err)
	}
	if err := store.CreateRelease(ctx, release.Record{
		ID: "rel-api-1", App: "api", ConfigCommit: "commit-2",
		ManifestJSON: `{"id":"rel-api-1","app":"api","image_ref":"ghcr.io/acme/api:v1.0.0","image_tag":"v1.0.0","services":{"api":{"image_ref":"ghcr.io/acme/api:v1.0.0","image_tag":"v1.0.0","env_hash":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}}`,
	}); err != nil {
		t.Fatalf("CreateRelease(api) error = %v", err)
	}
	service := usecase.NewService(store, nil, nil, nil)

	entries, err := service.ResolveBaselineReleases(ctx, map[string]string{
		"api":    "rel-api-1",
		"broken": "rel-broken",
	})
	if err != nil {
		t.Fatalf("ResolveBaselineReleases() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Name != "api" || len(entries[0].Services) != 1 {
		t.Fatalf("api entry = %+v", entries[0])
	}
	if entries[1].Name != "broken" || entries[1].ManifestError == "" {
		t.Fatalf("broken entry = %+v", entries[1])
	}
}

func TestCheckImagesRecordsDigestChanges(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	registry := fakeRegistry{digests: map[string]string{"ghcr.io/acme/api:v1.0.0": "sha256:first"}}
	service := usecase.NewService(store, github, registry, &fakeDeployer{})
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

func TestSyncConfigRecordsReleaseForChangedAppConfig(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	records, err := store.ListReleases(ctx, "api")
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("releases after attach = %d, want 1 (synced config must be deployable)", len(records))
	}
	if records[0].Status != release.StatusReady {
		t.Fatalf("release status = %q, want ready", records[0].Status)
	}

	if err := service.SyncConfig(ctx, adminUser()); err != nil {
		t.Fatalf("SyncConfig() error = %v", err)
	}
	records, err = store.ListReleases(ctx, "api")
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("releases after re-sync = %d, want 1 (no duplicates for unchanged config)", len(records))
	}

	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   appYAML("v2.0.0"),
		SHA:       "app-sha-2",
		CommitSHA: "commit-2",
	}
	github.head = "commit-2"
	if err := service.SyncConfig(ctx, adminUser()); err != nil {
		t.Fatalf("SyncConfig() after change error = %v", err)
	}
	records, err = store.ListReleases(ctx, "api")
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("releases after config change = %d, want 2", len(records))
	}
}

func TestCheckImagesResolvesSemverCandidates(t *testing.T) {
	tests := []struct {
		name          string
		pinnedTag     string
		tags          []string
		wantStatus    imagewatch.Status
		wantCandidate string
	}{
		{
			name:          "newer patch available",
			pinnedTag:     "1.2.0",
			tags:          []string{"1.2.0", "1.2.5", "1.3.0", "1.2", "latest"},
			wantStatus:    imagewatch.StatusUpdateAvailable,
			wantCandidate: "1.2.5",
		},
		{
			name:          "pinned tag is current",
			pinnedTag:     "1.2.5",
			tags:          []string{"1.2.0", "1.2.5"},
			wantStatus:    imagewatch.StatusReady,
			wantCandidate: "1.2.5",
		},
		{
			name:          "unpinned service adopts highest match",
			pinnedTag:     "",
			tags:          []string{"1.2.0", "1.2.5"},
			wantStatus:    imagewatch.StatusUpdateAvailable,
			wantCandidate: "1.2.5",
		},
		{
			name:       "no matching tags",
			pinnedTag:  "1.2.0",
			tags:       []string{"latest", "edge"},
			wantStatus: imagewatch.StatusInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store := tursoconn.NewMemoryStore()
			github := newFakeGitHub()
			github.files["apps/api/app.yml"] = usecase.GitHubFile{
				Path:      "apps/api/app.yml",
				Content:   semverAppYAML("~1.2", test.pinnedTag),
				SHA:       "app-sha",
				CommitSHA: "commit-1",
			}
			registry := fakeRegistry{tags: map[string][]string{"ghcr.io/acme/api": test.tags}}
			service := usecase.NewService(store, github, registry, &fakeDeployer{})
			if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
				t.Fatalf("AttachRepository() error = %v", err)
			}
			if err := service.CheckImages(ctx, adminUser()); err != nil {
				t.Fatalf("CheckImages() error = %v", err)
			}
			cursors, err := store.ListImageCursors(ctx)
			if err != nil {
				t.Fatalf("ListImageCursors() error = %v", err)
			}
			if len(cursors) != 1 {
				t.Fatalf("cursors = %d, want 1", len(cursors))
			}
			if cursors[0].Status != test.wantStatus {
				t.Fatalf("status = %q, want %q (error: %s)", cursors[0].Status, test.wantStatus, cursors[0].LastError)
			}
			if cursors[0].CandidateTag != test.wantCandidate {
				t.Fatalf("candidate = %q, want %q", cursors[0].CandidateTag, test.wantCandidate)
			}
			if cursors[0].Constraint != "~1.2" {
				t.Fatalf("constraint = %q, want ~1.2", cursors[0].Constraint)
			}
		})
	}
}

func TestCheckImagesMarksSemverCursorInvalidOnRegistryError(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   semverAppYAML("~1.2", "1.2.0"),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	service := usecase.NewService(store, github, fakeRegistry{}, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	if err := service.CheckImages(ctx, adminUser()); err != nil {
		t.Fatalf("CheckImages() error = %v", err)
	}
	cursors, err := store.ListImageCursors(ctx)
	if err != nil {
		t.Fatalf("ListImageCursors() error = %v", err)
	}
	if len(cursors) != 1 || cursors[0].Status != imagewatch.StatusInvalid {
		t.Fatalf("cursors = %+v, want one invalid cursor", cursors)
	}
	if cursors[0].LastError == "" {
		t.Fatal("invalid cursor should record the registry error")
	}
}

func TestCheckImagesMarksUnsupportedRegistryAsUntracked(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   appYAML("v1.0.0"),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	service := usecase.NewService(store, github, fakeRegistry{err: usecase.ErrUnsupportedRegistry}, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	if err := service.CheckImages(ctx, adminUser()); err != nil {
		t.Fatalf("CheckImages() error = %v", err)
	}
	cursors, err := store.ListImageCursors(ctx)
	if err != nil {
		t.Fatalf("ListImageCursors() error = %v", err)
	}
	if len(cursors) != 1 || cursors[0].Status != imagewatch.StatusUntracked {
		t.Fatalf("cursors = %+v, want one untracked cursor", cursors)
	}
	if cursors[0].LastError != "" {
		t.Fatalf("LastError = %q, want empty", cursors[0].LastError)
	}
}

func TestCheckImagesMarksSemverUnsupportedRegistryAsUntracked(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   semverAppYAML("~1.2", "1.2.0"),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	service := usecase.NewService(store, github, fakeRegistry{err: usecase.ErrUnsupportedRegistry}, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	if err := service.CheckImages(ctx, adminUser()); err != nil {
		t.Fatalf("CheckImages() error = %v", err)
	}
	cursors, err := store.ListImageCursors(ctx)
	if err != nil {
		t.Fatalf("ListImageCursors() error = %v", err)
	}
	if len(cursors) != 1 || cursors[0].Status != imagewatch.StatusUntracked {
		t.Fatalf("cursors = %+v, want one untracked cursor", cursors)
	}
	if cursors[0].LastError != "" {
		t.Fatalf("LastError = %q, want empty", cursors[0].LastError)
	}
}

func TestCommitImageBumpsCommitsPendingUpdates(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   semverAppYAML("~1.2", "1.2.0"),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	registry := fakeRegistry{
		tags:    map[string][]string{"ghcr.io/acme/api": {"1.2.0", "1.2.5", "latest"}},
		digests: map[string]string{"ghcr.io/acme/api:1.2.5": "sha256:bumped"},
	}
	service := usecase.NewService(store, github, registry, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	if err := service.CheckImages(ctx, adminUser()); err != nil {
		t.Fatalf("CheckImages() error = %v", err)
	}

	records, err := service.CommitImageBumps(ctx, adminUser())
	if err != nil {
		t.Fatalf("CommitImageBumps() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].App != "api" {
		t.Fatalf("record app = %q, want api", records[0].App)
	}

	appFile := github.files["apps/api/app.yml"]
	if !strings.Contains(appFile.Content, "image_tag: 1.2.5") {
		t.Fatalf("app.yml missing bumped tag:\n%s", appFile.Content)
	}
	if !strings.Contains(appFile.Content, "image_digest: sha256:bumped") {
		t.Fatalf("app.yml missing pinned digest:\n%s", appFile.Content)
	}
	if !strings.Contains(appFile.Content, "image_semver:") {
		t.Fatalf("app.yml lost the semver constraint:\n%s", appFile.Content)
	}

	cursors, err := store.ListImageCursors(ctx)
	if err != nil {
		t.Fatalf("ListImageCursors() error = %v", err)
	}
	if len(cursors) != 1 || cursors[0].Status != imagewatch.StatusReady {
		t.Fatalf("cursor after bump = %+v, want ready", cursors)
	}
	if cursors[0].Tag != "1.2.5" {
		t.Fatalf("cursor tag = %q, want 1.2.5", cursors[0].Tag)
	}

	again, err := service.CommitImageBumps(ctx, adminUser())
	if err != nil {
		t.Fatalf("CommitImageBumps() second error = %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second run records = %d, want 0 (idempotent)", len(again))
	}
}

func TestCommitImageBumpsBlocksOnDigestFailure(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	github := newFakeGitHub()
	github.files["apps/api/app.yml"] = usecase.GitHubFile{
		Path:      "apps/api/app.yml",
		Content:   semverAppYAML("~1.2", "1.2.0"),
		SHA:       "app-sha",
		CommitSHA: "commit-1",
	}
	registry := fakeRegistry{
		tags: map[string][]string{"ghcr.io/acme/api": {"1.2.5"}},
	}
	service := usecase.NewService(store, github, registry, &fakeDeployer{})
	if err := service.AttachRepository(ctx, adminUser(), configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("AttachRepository() error = %v", err)
	}
	if err := service.CheckImages(ctx, adminUser()); err != nil {
		t.Fatalf("CheckImages() error = %v", err)
	}

	records, err := service.CommitImageBumps(ctx, adminUser())
	if err != nil {
		t.Fatalf("CommitImageBumps() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %d, want 0 when digest resolution fails", len(records))
	}
	appFile := github.files["apps/api/app.yml"]
	if strings.Contains(appFile.Content, "1.2.5") {
		t.Fatalf("app.yml must not be bumped without a digest:\n%s", appFile.Content)
	}
	cursors, err := store.ListImageCursors(ctx)
	if err != nil {
		t.Fatalf("ListImageCursors() error = %v", err)
	}
	if len(cursors) != 1 || cursors[0].Status != imagewatch.StatusInvalid {
		t.Fatalf("cursor = %+v, want invalid", cursors)
	}
	if !strings.Contains(cursors[0].LastError, "resolve digest") {
		t.Fatalf("cursor error = %q, want digest resolution failure", cursors[0].LastError)
	}
}

func TestDeployReleaseRecordsSuccessfulAttempt(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedRuntimeConfig(t, store)
	deployer := &fakeDeployer{targets: []string{"node-1"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)
	if err := store.CreateRelease(ctx, release.Record{
		ID:           "rel-old",
		App:          "api",
		ConfigCommit: "commit-0",
		ManifestJSON: `{"id":"rel-old","app":"api","services":{"api":{"image_ref":"ghcr.io/acme/api:v0.9.0","env_hash":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}}`,
		Status:       release.StatusDeployed,
		ActorEmail:   adminUser().Email,
	}); err != nil {
		t.Fatalf("CreateRelease() old error = %v", err)
	}
	if err := store.CreateRelease(ctx, release.Record{
		ID:           "rel-1",
		App:          "api",
		ConfigCommit: "commit-1",
		ManifestJSON: `{"id":"rel-1","app":"api","services":{"api":{"image_ref":"ghcr.io/acme/api:v1.0.0","env_hash":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}}`,
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
	if !hasReleaseManifest(deployer.releaseManifests, "rel-old") || !hasReleaseManifest(deployer.releaseManifests, "rel-1") {
		t.Fatalf("release manifests = %+v, want current and previous release", deployer.releaseManifests)
	}
}

func TestDeployReleaseBackfillsMissingSourceCompose(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedRuntimeConfig(t, store)
	if err := store.SaveRepositorySettings(ctx, configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"}); err != nil {
		t.Fatalf("SaveRepositorySettings() error = %v", err)
	}
	if err := store.UpsertApp(ctx, configrepo.AppSnapshot{
		Name:    "api",
		RawYAML: sourceAppYAML(),
	}); err != nil {
		t.Fatalf("UpsertApp() error = %v", err)
	}
	if err := store.CreateRelease(ctx, release.Record{
		ID:           "rel-1",
		App:          "api",
		ConfigCommit: "commit-1",
		ManifestJSON: `{"id":"rel-1","app":"api"}`,
		Status:       release.StatusReady,
		ActorEmail:   adminUser().Email,
	}); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	github := newFakeGitHub()
	github.files["deploy/compose.yml"] = usecase.GitHubFile{
		Path:      "deploy/compose.yml",
		Content:   "services:\n  api:\n    image: ghcr.io/acme/api\n",
		SHA:       "source-compose-sha",
		CommitSHA: "source-commit",
	}
	deployer := &fakeDeployer{}
	service := usecase.NewService(store, github, fakeRegistry{}, deployer)

	if _, err := service.DeployRelease(ctx, adminUser(), "rel-1"); err != nil {
		t.Fatalf("DeployRelease() error = %v", err)
	}

	if len(deployer.config.Apps) != 1 || deployer.config.Apps[0].ComposeYAML == "" {
		t.Fatalf("deployer config did not include resolved compose: %+v", deployer.config.Apps)
	}
	snapshot, ok, err := store.App(ctx, "api")
	if err != nil || !ok {
		t.Fatalf("App() ok = %v error = %v", ok, err)
	}
	if snapshot.ComposeYAML == "" {
		t.Fatalf("compose was not persisted")
	}
}

func TestDeployReleaseDoesNotResolveUnreleasedAppCompose(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedRuntimeConfig(t, store)
	if err := store.UpsertApp(ctx, configrepo.AppSnapshot{
		Name:        "platform",
		RawYAML:     "kind: warpgate/app\nrelease:\n  services:\n    platform:\n      image: ghcr.io/acme/platform\n",
		ComposeYAML: "services:\n  platform:\n    image: ghcr.io/acme/platform\n",
	}); err != nil {
		t.Fatalf("UpsertApp() platform error = %v", err)
	}
	if err := store.UpsertApp(ctx, configrepo.AppSnapshot{
		Name:    "site",
		RawYAML: sourceAppYAML(),
	}); err != nil {
		t.Fatalf("UpsertApp() site error = %v", err)
	}
	if err := store.CreateRelease(ctx, release.Record{
		ID:           "rel-platform",
		App:          "platform",
		ConfigCommit: "commit-1",
		ManifestJSON: `{"id":"rel-platform","app":"platform"}`,
		Status:       release.StatusReady,
		ActorEmail:   adminUser().Email,
	}); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	deployer := &fakeDeployer{}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	if _, err := service.DeployRelease(ctx, adminUser(), "rel-platform"); err != nil {
		t.Fatalf("DeployRelease() error = %v", err)
	}

	for _, snapshot := range deployer.config.Apps {
		if snapshot.Name == "site" && snapshot.ComposeYAML != "" {
			t.Fatalf("unreleased source app compose was resolved")
		}
	}
}

func seedStackApps(t *testing.T, store *tursoconn.MemoryStore, releases map[string]string) {
	t.Helper()
	ctx := context.Background()
	seedRuntimeConfig(t, store)
	apps := make([]string, 0, len(releases))
	for app := range releases {
		apps = append(apps, app)
	}
	sort.Strings(apps)
	for _, app := range apps {
		if err := store.UpsertApp(ctx, configrepo.AppSnapshot{Name: app, FileSHA: app + "-sha", RawYAML: appYAML("v1.0.0")}); err != nil {
			t.Fatalf("UpsertApp(%s) error = %v", app, err)
		}
		if err := store.CreateRelease(ctx, release.Record{
			ID:           releases[app],
			App:          app,
			ConfigCommit: "commit-1",
			ManifestJSON: `{}`,
			Status:       release.StatusReady,
			ActorEmail:   adminUser().Email,
		}); err != nil {
			t.Fatalf("CreateRelease(%s) error = %v", app, err)
		}
	}
}

func TestDeployStackAdvancesBaselineOnSuccess(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"})
	deployer := &fakeDeployer{targets: []string{"node-1"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	attempt, err := service.DeployStack(ctx, adminUser(), false)
	if err != nil {
		t.Fatalf("DeployStack() error = %v", err)
	}
	if attempt.Status != stackstate.StatusSucceeded {
		t.Fatalf("attempt status = %q, want succeeded", attempt.Status)
	}
	if len(deployer.deployed) != 2 || deployer.deployed[0] != "rel-api-1" || deployer.deployed[1] != "rel-worker-1" {
		t.Fatalf("deployed = %v, want [rel-api-1 rel-worker-1]", deployer.deployed)
	}
	state, err := store.StackState(ctx)
	if err != nil {
		t.Fatalf("StackState() error = %v", err)
	}
	if state.LastHealthy.Releases["api"] != "rel-api-1" || state.LastHealthy.Releases["worker"] != "rel-worker-1" {
		t.Fatalf("baseline = %+v, want both releases", state.LastHealthy.Releases)
	}
	if state.LastHealthy.ClusterFileSHA == "" {
		t.Fatal("baseline cluster file sha must be recorded")
	}
	if state.LastAttempt == nil || state.LastAttempt.FinishedAt == nil {
		t.Fatalf("last attempt = %+v, want finished attempt", state.LastAttempt)
	}
}

func TestDeployStackRevertsToBaselineOnFailure(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"})
	baseline := stackstate.Snapshot{Releases: map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"}}
	if err := store.SaveStackState(ctx, stackstate.State{LastHealthy: baseline}); err != nil {
		t.Fatalf("SaveStackState() error = %v", err)
	}
	for app, id := range map[string]string{"api": "rel-api-2", "worker": "rel-worker-2"} {
		if err := store.CreateRelease(ctx, release.Record{
			ID: id, App: app, ConfigCommit: "commit-2", ManifestJSON: `{}`,
			Status: release.StatusReady, ActorEmail: adminUser().Email,
		}); err != nil {
			t.Fatalf("CreateRelease(%s) error = %v", id, err)
		}
	}
	deployer := &fakeDeployer{
		targets:      []string{"node-1"},
		failReleases: map[string]error{"rel-worker-2": errors.New("health check failed")},
	}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	attempt, err := service.DeployStack(ctx, adminUser(), false)
	if err == nil {
		t.Fatal("DeployStack() expected error, got nil")
	}
	if attempt.Status != stackstate.StatusReverted {
		t.Fatalf("attempt status = %q, want reverted", attempt.Status)
	}
	if attempt.FailedApp != "worker" {
		t.Fatalf("failed app = %q, want worker", attempt.FailedApp)
	}
	want := []string{"rel-api-2", "rel-worker-2", "rel-api-1", "rel-worker-1"}
	if len(deployer.deployed) != len(want) {
		t.Fatalf("deployed = %v, want %v", deployer.deployed, want)
	}
	for index := range want {
		if deployer.deployed[index] != want[index] {
			t.Fatalf("deployed = %v, want %v", deployer.deployed, want)
		}
	}
	state, err := store.StackState(ctx)
	if err != nil {
		t.Fatalf("StackState() error = %v", err)
	}
	if state.LastHealthy.Releases["api"] != "rel-api-1" || state.LastHealthy.Releases["worker"] != "rel-worker-1" {
		t.Fatalf("baseline = %+v, must not advance on failure", state.LastHealthy.Releases)
	}
}

func TestDeployStackHaltsWithoutBaseline(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1"})
	deployer := &fakeDeployer{
		targets:      []string{"node-1"},
		failReleases: map[string]error{"rel-api-1": errors.New("health check failed")},
	}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	attempt, err := service.DeployStack(ctx, adminUser(), false)
	if err == nil {
		t.Fatal("DeployStack() expected error, got nil")
	}
	if attempt.Status != stackstate.StatusFailed {
		t.Fatalf("attempt status = %q, want failed", attempt.Status)
	}
	if len(deployer.deployed) != 1 {
		t.Fatalf("deployed = %v, want no revert deploys without a baseline", deployer.deployed)
	}
}

func TestDeployStackFlagsRevertFailure(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1"})
	if err := store.SaveStackState(ctx, stackstate.State{
		LastHealthy: stackstate.Snapshot{Releases: map[string]string{"api": "rel-api-1"}},
	}); err != nil {
		t.Fatalf("SaveStackState() error = %v", err)
	}
	if err := store.CreateRelease(ctx, release.Record{
		ID: "rel-api-2", App: "api", ConfigCommit: "commit-2", ManifestJSON: `{}`,
		Status: release.StatusReady, ActorEmail: adminUser().Email,
	}); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	deployer := &fakeDeployer{
		targets: []string{"node-1"},
		failReleases: map[string]error{
			"rel-api-2": errors.New("health check failed"),
			"rel-api-1": errors.New("node unreachable"),
		},
	}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	attempt, err := service.DeployStack(ctx, adminUser(), false)
	if err == nil {
		t.Fatal("DeployStack() expected error, got nil")
	}
	if attempt.Status != stackstate.StatusRevertFailed {
		t.Fatalf("attempt status = %q, want revert-failed", attempt.Status)
	}
	if attempt.RevertError == "" {
		t.Fatal("revert error must be recorded for operator attention")
	}
}

func TestRollbackStackRedeploysBaseline(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"})
	if err := store.SaveStackState(ctx, stackstate.State{
		LastHealthy: stackstate.Snapshot{Releases: map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"}},
	}); err != nil {
		t.Fatalf("SaveStackState() error = %v", err)
	}
	deployer := &fakeDeployer{targets: []string{"node-1"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	attempt, err := service.RollbackStack(ctx, adminUser())
	if err != nil {
		t.Fatalf("RollbackStack() error = %v", err)
	}
	if attempt.Status != stackstate.StatusReverted {
		t.Fatalf("attempt status = %q, want reverted", attempt.Status)
	}
	if len(deployer.deployed) != 2 || deployer.deployed[0] != "rel-api-1" || deployer.deployed[1] != "rel-worker-1" {
		t.Fatalf("deployed = %v, want baseline releases in app order", deployer.deployed)
	}
}

func TestBuildStackDeployPlanSkipsUnchangedRelease(t *testing.T) {
	plan := usecase.BuildStackDeployPlanForTest(
		map[string]string{"api": "rel-2", "web": "rel-1"},
		stackstate.Snapshot{
			Releases:       map[string]string{"api": "rel-1", "web": "rel-1"},
			ClusterFileSHA: "cluster-sha",
			AppConfigSHAs:  map[string]string{"api": "api-sha", "web": "web-sha"},
		},
		"cluster-sha",
		[]configrepo.AppSnapshot{
			{Name: "api", FileSHA: "api-sha"},
			{Name: "web", FileSHA: "web-sha"},
		},
		false,
	)
	if len(plan.ToDeploy) != 1 || plan.ToDeploy["api"] != "rel-2" {
		t.Fatalf("to deploy = %#v, want only api rel-2", plan.ToDeploy)
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0] != "web" {
		t.Fatalf("skipped = %#v, want [web]", plan.Skipped)
	}
}

func TestBuildStackDeployPlanClusterChangeDeploysAll(t *testing.T) {
	plan := usecase.BuildStackDeployPlanForTest(
		map[string]string{"api": "rel-1", "web": "rel-1"},
		stackstate.Snapshot{
			Releases:       map[string]string{"api": "rel-1", "web": "rel-1"},
			ClusterFileSHA: "cluster-sha-old",
			AppConfigSHAs:  map[string]string{"api": "api-sha", "web": "web-sha"},
		},
		"cluster-sha-new",
		[]configrepo.AppSnapshot{
			{Name: "api", FileSHA: "api-sha"},
			{Name: "web", FileSHA: "web-sha"},
		},
		false,
	)
	if len(plan.ToDeploy) != 2 {
		t.Fatalf("to deploy = %#v, want both apps", plan.ToDeploy)
	}
	if len(plan.Skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", plan.Skipped)
	}
}

func TestBuildStackDeployPlanAppConfigChange(t *testing.T) {
	plan := usecase.BuildStackDeployPlanForTest(
		map[string]string{"api": "rel-1"},
		stackstate.Snapshot{
			Releases:       map[string]string{"api": "rel-1"},
			ClusterFileSHA: "cluster-sha",
			AppConfigSHAs:  map[string]string{"api": "api-sha-old"},
		},
		"cluster-sha",
		[]configrepo.AppSnapshot{{Name: "api", FileSHA: "api-sha-new"}},
		false,
	)
	if len(plan.ToDeploy) != 1 {
		t.Fatalf("to deploy = %#v, want api", plan.ToDeploy)
	}
}

func TestDeployStackSkipsUnchangedAppsOnSecondRun(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"})
	deployer := &fakeDeployer{targets: []string{"node-1"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	if _, err := service.DeployStack(ctx, adminUser(), false); err != nil {
		t.Fatalf("first DeployStack() error = %v", err)
	}
	deployer.deployed = nil
	attempt, err := service.DeployStack(ctx, adminUser(), false)
	if err != nil {
		t.Fatalf("second DeployStack() error = %v", err)
	}
	if attempt.Status != stackstate.StatusSucceeded {
		t.Fatalf("attempt status = %q, want succeeded", attempt.Status)
	}
	if len(deployer.deployed) != 0 {
		t.Fatalf("deployed = %v, want no deploy calls on unchanged stack", deployer.deployed)
	}
}

func TestDeployStackNoOpWhenUnchanged(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1"})
	if err := store.SaveStackState(ctx, stackstate.State{
		LastHealthy: stackstate.Snapshot{
			Releases:       map[string]string{"api": "rel-api-1"},
			ClusterFileSHA: "cluster-sha",
			AppConfigSHAs:  map[string]string{"api": "api-sha"},
		},
	}); err != nil {
		t.Fatalf("SaveStackState() error = %v", err)
	}
	deployer := &fakeDeployer{targets: []string{"node-1"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	attempt, err := service.DeployStack(ctx, adminUser(), false)
	if err != nil {
		t.Fatalf("DeployStack() error = %v", err)
	}
	if len(deployer.deployed) != 0 {
		t.Fatalf("deployed = %v, want no-op", deployer.deployed)
	}
	if len(attempt.SkippedApps) != 1 || attempt.SkippedApps[0] != "api" {
		t.Fatalf("skipped apps = %v, want [api]", attempt.SkippedApps)
	}
}

func TestDeployStackForceRedeploysAll(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"})
	if err := store.SaveStackState(ctx, stackstate.State{
		LastHealthy: stackstate.Snapshot{
			Releases:       map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"},
			ClusterFileSHA: "cluster-sha",
			AppConfigSHAs:  map[string]string{"api": "api-sha", "worker": "worker-sha"},
		},
	}); err != nil {
		t.Fatalf("SaveStackState() error = %v", err)
	}
	deployer := &fakeDeployer{targets: []string{"node-1"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	attempt, err := service.DeployStack(ctx, adminUser(), true)
	if err != nil {
		t.Fatalf("DeployStack() error = %v", err)
	}
	if len(deployer.deployed) != 2 {
		t.Fatalf("deployed = %v, want both apps forced", deployer.deployed)
	}
	if !attempt.Forced {
		t.Fatal("attempt.Forced must be true for forced deploy")
	}
	if len(attempt.SkippedApps) != 0 {
		t.Fatalf("skipped apps = %v, want none when forced", attempt.SkippedApps)
	}
}

func TestDeployStackClusterChangeRedeploysAll(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"})
	if err := store.SaveStackState(ctx, stackstate.State{
		LastHealthy: stackstate.Snapshot{
			Releases:       map[string]string{"api": "rel-api-1", "worker": "rel-worker-1"},
			ClusterFileSHA: "cluster-sha-old",
			AppConfigSHAs:  map[string]string{"api": "api-sha", "worker": "worker-sha"},
		},
	}); err != nil {
		t.Fatalf("SaveStackState() error = %v", err)
	}
	if err := store.SaveClusterConfig(ctx, configrepo.ClusterSnapshot{
		Path:    "prod/cluster.yml",
		FileSHA: "cluster-sha-new",
		RawYAML: clusterYAML(),
	}); err != nil {
		t.Fatalf("SaveClusterConfig() error = %v", err)
	}
	deployer := &fakeDeployer{targets: []string{"node-1"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	if _, err := service.DeployStack(ctx, adminUser(), false); err != nil {
		t.Fatalf("DeployStack() error = %v", err)
	}
	if len(deployer.deployed) != 2 {
		t.Fatalf("deployed = %v, want both apps after cluster change", deployer.deployed)
	}
}

func TestRollbackStackRequiresBaseline(t *testing.T) {
	ctx := context.Background()
	store := tursoconn.NewMemoryStore()
	seedStackApps(t, store, map[string]string{"api": "rel-api-1"})
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, &fakeDeployer{})

	if _, err := service.RollbackStack(ctx, adminUser()); err == nil {
		t.Fatal("RollbackStack() expected error without a baseline")
	}
}

func TestRuntimeStatusRequiresAdmin(t *testing.T) {
	service := usecase.NewService(tursoconn.NewMemoryStore(), newFakeGitHub(), fakeRegistry{}, &fakeDeployer{})

	_, err := service.RuntimeStatus(context.Background(), identity.User{Email: "viewer@example.com"})

	if err == nil {
		t.Fatalf("RuntimeStatus() error = nil, want admin error")
	}
}

func TestConfigNodesRequiresAdmin(t *testing.T) {
	service := usecase.NewService(tursoconn.NewMemoryStore(), newFakeGitHub(), fakeRegistry{}, &fakeDeployer{})

	_, err := service.ConfigNodes(context.Background(), identity.User{Email: "viewer@example.com"})

	if err == nil {
		t.Fatalf("ConfigNodes() error = nil, want admin error")
	}
}

func TestRuntimeStatusReturnsDeployerState(t *testing.T) {
	expected := usecase.RuntimeStatus{
		Nodes: []usecase.RuntimeNode{{ID: "node-1", Reachable: true}},
		Apps:  []usecase.RuntimeAppStatus{{App: "api", NodeID: "node-1", State: "healthy"}},
	}
	store := tursoconn.NewMemoryStore()
	if err := store.SaveRepositorySettings(context.Background(), configrepo.RepositorySettings{Path: "prod"}); err != nil {
		t.Fatalf("SaveRepositorySettings() error = %v", err)
	}
	seedRuntimeConfig(t, store)
	deployer := &fakeDeployer{runtime: expected}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	status, err := service.RuntimeStatus(context.Background(), adminUser())
	if err != nil {
		t.Fatalf("RuntimeStatus() error = %v", err)
	}
	if deployer.config.RepositoryPath != "prod" {
		t.Fatalf("repository path = %q, want prod", deployer.config.RepositoryPath)
	}
	if deployer.config.Cluster.RawYAML == "" {
		t.Fatalf("cluster snapshot was not passed to deployer")
	}
	if len(status.Nodes) != 1 || status.Nodes[0].ID != "node-1" {
		t.Fatalf("nodes = %+v", status.Nodes)
	}
	if len(status.Apps) != 1 || status.Apps[0].State != "healthy" {
		t.Fatalf("apps = %+v", status.Apps)
	}
}

func TestLogsDefaultsTailAndTrimsInput(t *testing.T) {
	store := tursoconn.NewMemoryStore()
	seedRuntimeConfig(t, store)
	deployer := &capturingDeployer{logs: usecase.LogsResult{Output: "[api] ready\n"}}
	service := usecase.NewService(store, newFakeGitHub(), fakeRegistry{}, deployer)

	result, err := service.Logs(context.Background(), adminUser(), usecase.LogsInput{
		NodeID: " node-1 ",
		App:    " api ",
		Grep:   " ready ",
	})
	if err != nil {
		t.Fatalf("Logs() error = %v", err)
	}
	if result.Output != "[api] ready\n" {
		t.Fatalf("output = %q", result.Output)
	}
	if deployer.input.NodeID != "node-1" || deployer.input.App != "api" || deployer.input.Grep != "ready" {
		t.Fatalf("input = %+v", deployer.input)
	}
	if deployer.input.Tail != 100 {
		t.Fatalf("tail = %d, want 100", deployer.input.Tail)
	}
}

func TestLogsRejectsInvalidInput(t *testing.T) {
	service := usecase.NewService(tursoconn.NewMemoryStore(), newFakeGitHub(), fakeRegistry{}, &fakeDeployer{})

	if _, err := service.Logs(context.Background(), adminUser(), usecase.LogsInput{}); err == nil {
		t.Fatalf("Logs() node error = nil")
	}
	if _, err := service.Logs(context.Background(), adminUser(), usecase.LogsInput{NodeID: "node-1", Tail: 2001}); err == nil {
		t.Fatalf("Logs() tail error = nil")
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
			"prod/apps/api/vector.yaml": {
				Path:      "prod/apps/api/vector.yaml",
				Content:   "sources: {}\n",
				SHA:       "prod-vector-sha",
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

// ListAppExtraFiles returns fake flat app config files under the configured root.
func (f *fakeGitHub) ListAppExtraFiles(_ context.Context, settings configrepo.RepositorySettings, appName string, _ string) ([]usecase.GitHubFile, error) {
	prefix := "apps/" + appName + "/"
	root := strings.Trim(strings.TrimSpace(settings.Path), "/")
	if root != "" {
		prefix = root + "/" + prefix
	}
	var files []usecase.GitHubFile
	for name, file := range f.files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if strings.Contains(rel, "/") {
			continue
		}
		if !config.IsDeployExtraFile(rel) {
			continue
		}
		files = append(files, file)
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

// WriteFiles updates fake repository files in one commit.
func (f *fakeGitHub) WriteFiles(_ context.Context, input usecase.WriteFilesInput) ([]usecase.GitHubFile, error) {
	written := make([]usecase.GitHubFile, 0, len(input.Files))
	commitSHA := "commit-next"
	for index, change := range input.Files {
		file := usecase.GitHubFile{
			Path:      change.Path,
			Content:   change.Content,
			SHA:       "sha-next-" + strings.ReplaceAll(change.Path, "/", "-"),
			CommitSHA: commitSHA,
		}
		if index == 0 {
			file.SHA = "app-sha-next"
		}
		f.files[change.Path] = file
		written = append(written, file)
	}
	f.head = commitSHA
	return written, nil
}

type fakeRegistry struct {
	digests map[string]string
	tags    map[string][]string
	err     error
}

// ResolveDigest returns a fake image digest.
func (f fakeRegistry) ResolveDigest(_ context.Context, image string, tag string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	digest, ok := f.digests[image+":"+tag]
	if !ok {
		return "", errors.New("digest not found")
	}
	return digest, nil
}

// ListTags returns fake registry tags.
func (f fakeRegistry) ListTags(_ context.Context, image string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	tags, ok := f.tags[image]
	if !ok {
		return nil, errors.New("tags not found")
	}
	return tags, nil
}

type fakeDeployer struct {
	targets          []string
	err              error
	failReleases     map[string]error
	deployed         []string
	nodes            []usecase.ConfigNode
	runtime          usecase.RuntimeStatus
	config           usecase.RuntimeConfigInput
	releaseManifests []usecase.ReleaseManifestInput
}

// DeployRelease returns a fake deployment result.
func (f *fakeDeployer) DeployRelease(_ context.Context, input usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	f.config = input.Config
	f.releaseManifests = input.ReleaseManifests
	f.deployed = append(f.deployed, input.ReleaseID)
	if err, ok := f.failReleases[input.ReleaseID]; ok {
		return usecase.DeployResult{}, err
	}
	return usecase.DeployResult{Targets: f.targets}, f.err
}

// ConfigNodes returns fake config nodes.
func (f *fakeDeployer) ConfigNodes(_ context.Context, config usecase.RuntimeConfigInput) ([]usecase.ConfigNode, error) {
	f.config = config
	return f.nodes, f.err
}

// RuntimeStatus returns fake runtime status.
func (f *fakeDeployer) RuntimeStatus(_ context.Context, config usecase.RuntimeConfigInput) (usecase.RuntimeStatus, error) {
	f.config = config
	return f.runtime, f.err
}

// AppRuntimeStatus returns fake app runtime status.
func (f *fakeDeployer) AppRuntimeStatus(_ context.Context, config usecase.RuntimeConfigInput, _ string) ([]usecase.RuntimeNodeStatus, error) {
	f.config = config
	return nil, f.err
}

// Logs returns fake logs.
func (f *fakeDeployer) Logs(_ context.Context, config usecase.RuntimeConfigInput, _ usecase.LogsInput) (usecase.LogsResult, error) {
	f.config = config
	return usecase.LogsResult{}, f.err
}

type capturingDeployer struct {
	input usecase.LogsInput
	logs  usecase.LogsResult
}

// DeployRelease returns an empty deployment result.
func (f *capturingDeployer) DeployRelease(context.Context, usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	return usecase.DeployResult{}, nil
}

// ConfigNodes returns empty config nodes.
func (f *capturingDeployer) ConfigNodes(context.Context, usecase.RuntimeConfigInput) ([]usecase.ConfigNode, error) {
	return nil, nil
}

// RuntimeStatus returns an empty runtime status.
func (f *capturingDeployer) RuntimeStatus(context.Context, usecase.RuntimeConfigInput) (usecase.RuntimeStatus, error) {
	return usecase.RuntimeStatus{}, nil
}

// AppRuntimeStatus returns an empty app runtime status.
func (f *capturingDeployer) AppRuntimeStatus(context.Context, usecase.RuntimeConfigInput, string) ([]usecase.RuntimeNodeStatus, error) {
	return nil, nil
}

// Logs captures the log input and returns fake logs.
func (f *capturingDeployer) Logs(_ context.Context, _ usecase.RuntimeConfigInput, input usecase.LogsInput) (usecase.LogsResult, error) {
	f.input = input
	return f.logs, nil
}

func seedRuntimeConfig(t *testing.T, store *tursoconn.MemoryStore) {
	t.Helper()
	if err := store.SaveRepositorySettings(context.Background(), configrepo.RepositorySettings{
		Owner:  "acme",
		Repo:   "infra",
		Branch: "main",
		Path:   "prod",
	}); err != nil {
		t.Fatalf("SaveRepositorySettings() error = %v", err)
	}
	if err := store.SaveClusterConfig(context.Background(), configrepo.ClusterSnapshot{
		Path:    "prod/cluster.yml",
		FileSHA: "cluster-sha",
		RawYAML: clusterYAML(),
	}); err != nil {
		t.Fatalf("SaveClusterConfig() error = %v", err)
	}
}

func hasReleaseManifest(manifests []usecase.ReleaseManifestInput, id string) bool {
	for _, manifest := range manifests {
		if manifest.ID == id && manifest.ManifestJSON != "" {
			return true
		}
	}
	return false
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

func semverAppYAML(constraint string, tag string) string {
	yaml := `kind: warpgate/app
targets: [node-1]
release:
  services:
    api:
      image: ghcr.io/acme/api
      image_semver: "` + constraint + `"
`
	if tag != "" {
		yaml += "      image_tag: " + tag + "\n"
	}
	return yaml + `      port: 8080
      expose:
        public:
          domains: [api.example.com]
`
}

func multiServiceAppYAML(apiTag string, workerTag string) string {
	return `kind: warpgate/app
targets: [node-1]
release:
  services:
    api:
      image: ghcr.io/acme/api
      image_tag: ` + apiTag + `
      port: 8080
      expose:
        public:
          domains: [api.example.com]
    worker:
      image: ghcr.io/acme/worker
      image_tag: ` + workerTag + `
`
}

func sourceAppYAML() string {
	return `kind: warpgate/app
compose_ref: master
source:
  repo: github.com/pangobit/brighter
  compose_path: deploy/compose.yml
targets: [node-1]
release:
  services:
    api:
      image: ghcr.io/acme/api
      image_tag: v1.0.0
`
}
