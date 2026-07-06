package warpd

import (
	"context"
	"fmt"
	"strings"
	"time"

	sshapi "github.com/pangobit/warpgate/warpd/api/ssh"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/audit"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/internal/stackstate"
	"github.com/pangobit/warpgate/warpd/usecase"
)

const (
	// PreviewScenarioMixed shows a representative operator dashboard.
	PreviewScenarioMixed = "mixed"
	// PreviewScenarioFailure shows long errors and an unhealthy stack attempt.
	PreviewScenarioFailure = "failure"
	// PreviewScenarioEmpty shows the first-run empty state.
	PreviewScenarioEmpty = "empty"
)

// PreviewConfig selects the local operator TUI preview state.
type PreviewConfig struct {
	// Scenario is the seeded UI state to show.
	Scenario string
}

// RunPreview starts the operator TUI locally with in-memory fixture data.
func RunPreview(ctx context.Context, cfg PreviewConfig) error {
	service, _, err := newPreviewService(ctx, cfg.Scenario)
	if err != nil {
		return err
	}
	return sshapi.RunLocal(service, previewActor(), func() {})
}

func newPreviewService(ctx context.Context, scenario string) (*usecase.Service, *previewDeployer, error) {
	scenario = previewScenario(scenario)
	if err := validatePreviewScenario(scenario); err != nil {
		return nil, nil, err
	}
	store := tursoconn.NewMemoryStore()
	deployer := previewDeployerForScenario(scenario)
	service := usecase.NewService(store, nil, nil, deployer)
	if err := seedPreviewStore(ctx, store, scenario); err != nil {
		return nil, nil, err
	}
	return service, deployer, nil
}

func previewScenario(scenario string) string {
	if strings.TrimSpace(scenario) == "" {
		return PreviewScenarioMixed
	}
	return strings.TrimSpace(scenario)
}

func validatePreviewScenario(scenario string) error {
	switch scenario {
	case PreviewScenarioMixed, PreviewScenarioFailure, PreviewScenarioEmpty:
		return nil
	default:
		return fmt.Errorf("unknown preview scenario %q (want mixed, failure, or empty)", scenario)
	}
}

func seedPreviewStore(ctx context.Context, store *tursoconn.MemoryStore, scenario string) error {
	now := time.Now().UTC()
	if scenario == PreviewScenarioEmpty {
		return store.SaveConfigCursor(ctx, configrepo.SyncCursor{
			LastCheckedAt: now.Add(-2 * time.Minute),
			LastError:     "preview has no repository attached",
		})
	}
	if err := seedPreviewRepository(ctx, store, now); err != nil {
		return err
	}
	if err := seedPreviewApps(ctx, store, now); err != nil {
		return err
	}
	if err := seedPreviewReleases(ctx, store, now); err != nil {
		return err
	}
	if err := seedPreviewImages(ctx, store, scenario, now); err != nil {
		return err
	}
	if err := seedPreviewStack(ctx, store, scenario, now); err != nil {
		return err
	}
	return seedPreviewAudit(ctx, store, scenario, now)
}

func seedPreviewRepository(ctx context.Context, store *tursoconn.MemoryStore, now time.Time) error {
	if err := store.SaveRepositorySettings(ctx, configrepo.RepositorySettings{
		Owner:      "acme",
		Repo:       "infra",
		Branch:     "main",
		Path:       "prod",
		AttachedAt: now.Add(-48 * time.Hour),
	}); err != nil {
		return err
	}
	if err := store.SaveConfigCursor(ctx, configrepo.SyncCursor{
		LastObservedCommit: "abcdef1234567890abcdef1234567890abcdef12",
		LastCheckedAt:      now.Add(-3 * time.Minute),
	}); err != nil {
		return err
	}
	return store.SaveClusterConfig(ctx, configrepo.ClusterSnapshot{
		Path:         "prod/cluster.yml",
		ConfigCommit: "abcdef1234567890abcdef1234567890abcdef12",
		FileSHA:      "cluster-sha",
		RawYAML:      previewClusterYAML,
		UpdatedAt:    now.Add(-3 * time.Minute),
	})
}

func seedPreviewApps(ctx context.Context, store *tursoconn.MemoryStore, now time.Time) error {
	for _, app := range []configrepo.AppSnapshot{
		{
			Name:         "api",
			Path:         "prod/apps/api/app.yml",
			ConfigCommit: "abcdef1234567890abcdef1234567890abcdef12",
			FileSHA:      "api-sha",
			RawYAML:      previewAPIAppYAML,
			ComposeYAML:  previewAPIComposeYAML,
			UpdatedAt:    now.Add(-3 * time.Minute),
		},
		{
			Name:         "web",
			Path:         "prod/apps/web/app.yml",
			ConfigCommit: "abcdef1234567890abcdef1234567890abcdef12",
			FileSHA:      "web-sha",
			RawYAML:      previewWebAppYAML,
			ComposeYAML:  previewWebComposeYAML,
			UpdatedAt:    now.Add(-3 * time.Minute),
		},
	} {
		if err := store.UpsertApp(ctx, app); err != nil {
			return err
		}
	}
	return nil
}

func seedPreviewReleases(ctx context.Context, store *tursoconn.MemoryStore, now time.Time) error {
	records := []release.Record{
		previewRelease("rel-api-1", "api", "1.2.0", release.StatusDeployed, now.Add(-2*time.Hour)),
		previewRelease("rel-api-2", "api", "1.2.5", release.StatusReady, now.Add(-10*time.Minute)),
		previewRelease("rel-web-1", "web", "3.4.1", release.StatusDeployed, now.Add(-2*time.Hour)),
	}
	for _, record := range records {
		if err := store.CreateRelease(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func previewRelease(id string, app string, tag string, status release.Status, createdAt time.Time) release.Record {
	return release.Record{
		ID:           id,
		App:          app,
		ConfigCommit: "abcdef1234567890abcdef1234567890abcdef12",
		ManifestJSON: fmt.Sprintf(`{"id":%q,"app":%q,"services":{%q:{"image_ref":"ghcr.io/acme/%s:%s","env_hash":"sha256:preview"}}}`,
			id, app, app, app, tag),
		RawYAML:    fmt.Sprintf("preview release %s for %s", id, app),
		Status:     status,
		ActorEmail: "daemon@warpgate",
		CreatedAt:  createdAt,
	}
}

func seedPreviewImages(ctx context.Context, store *tursoconn.MemoryStore, scenario string, now time.Time) error {
	cursors := []imagewatch.Cursor{
		{
			App:            "api",
			Service:        "api",
			Image:          "ghcr.io/acme/api",
			Tag:            "1.2.0",
			Constraint:     "~1.2",
			CandidateTag:   "1.2.5",
			Status:         imagewatch.StatusUpdateAvailable,
			LastDigest:     "sha256:preview-api-120",
			LastCheckedAt:  now.Add(-6 * time.Minute),
			PreviousDigest: "sha256:preview-api-119",
		},
		{
			App:           "web",
			Service:       "web",
			Image:         "ghcr.io/acme/web",
			Tag:           "3.4.1",
			Status:        imagewatch.StatusReady,
			LastDigest:    "sha256:preview-web-341",
			LastCheckedAt: now.Add(-6 * time.Minute),
		},
	}
	if scenario == PreviewScenarioFailure {
		cursors = append(cursors, imagewatch.Cursor{
			App:           "worker",
			Service:       "worker",
			Image:         "ghcr.io/acme/worker",
			Tag:           "latest",
			Status:        imagewatch.StatusInvalid,
			LastCheckedAt: now.Add(-6 * time.Minute),
			LastError:     strings.Repeat("registry timeout while resolving ghcr.io/acme/worker:latest ", 5),
		})
	}
	for _, cursor := range cursors {
		if err := store.SaveImageCursor(ctx, cursor); err != nil {
			return err
		}
	}
	return nil
}

func seedPreviewStack(ctx context.Context, store *tursoconn.MemoryStore, scenario string, now time.Time) error {
	finished := now.Add(-8 * time.Minute)
	attempt := &stackstate.Attempt{
		ID:         "stack-preview-1",
		Status:     stackstate.StatusSucceeded,
		Releases:   map[string]string{"api": "rel-api-1", "web": "rel-web-1"},
		ActorEmail: "ray@ssh",
		StartedAt:  now.Add(-12 * time.Minute),
		FinishedAt: &finished,
	}
	if scenario == PreviewScenarioFailure {
		attempt.Status = stackstate.StatusRevertFailed
		attempt.Releases = map[string]string{"api": "rel-api-2", "web": "rel-web-1"}
		attempt.FailedApp = "api"
		attempt.Error = strings.Repeat("health check failed on node-a: /health returned 503 after deploy ", 4)
		attempt.RevertError = strings.Repeat("rollback could not restart api-blue because docker compose returned exit status 1 ", 3)
	}
	return store.SaveStackState(ctx, stackstate.State{
		LastHealthy: stackstate.Snapshot{
			Releases:  map[string]string{"api": "rel-api-1", "web": "rel-web-1"},
			UpdatedAt: now.Add(-2 * time.Hour),
		},
		LastAttempt: attempt,
	})
}

func seedPreviewAudit(ctx context.Context, store *tursoconn.MemoryStore, scenario string, now time.Time) error {
	events := []audit.Event{
		previewAudit("audit-1", "repo.attach", "ray@ssh", "acme/infra attached", now.Add(-48*time.Hour)),
		previewAudit("audit-2", "config.sync", "daemon@warpgate", "config synced at abcdef123456", now.Add(-2*time.Hour)),
		previewAudit("audit-3", "images.sync", "daemon@warpgate", "image metadata checked", now.Add(-6*time.Minute)),
		previewAudit("audit-4", "stack.deploy.succeeded", "ray@ssh", "2 apps deployed", now.Add(-8*time.Minute)),
	}
	if scenario == PreviewScenarioFailure {
		events = append(events,
			previewAudit("audit-5", "stack.revert.failed", "ray@ssh", strings.Repeat("api failed health checks and rollback requires operator attention ", 4), now.Add(-4*time.Minute)),
			previewAudit("audit-6", "images.sync", "daemon@warpgate", "worker image could not be checked", now.Add(-3*time.Minute)),
		)
	}
	for _, event := range events {
		if err := store.AddAuditEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func previewAudit(id string, eventType string, actor string, message string, createdAt time.Time) audit.Event {
	return audit.Event{
		ID:         id,
		Type:       eventType,
		ActorEmail: actor,
		Message:    message,
		CreatedAt:  createdAt,
	}
}

func previewDeployerForScenario(scenario string) *previewDeployer {
	deployer := &previewDeployer{
		targets: []string{"node-a", "node-b"},
	}
	if scenario == PreviewScenarioFailure {
		deployer.failApp = "api"
	}
	return deployer
}

func previewActor() identity.User {
	return identity.User{
		Email:        "preview@warpgate",
		DisplayName:  "Warpgate Preview",
		Capabilities: []string{identity.AdminCapability},
	}
}

type previewDeployer struct {
	targets  []string
	failApp  string
	deployed []string
}

// DeployRelease records a preview deployment without contacting a node.
func (d *previewDeployer) DeployRelease(_ context.Context, input usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	d.deployed = append(d.deployed, input.App)
	if d.failApp == input.App {
		return usecase.DeployResult{Targets: d.targets}, fmt.Errorf("preview deploy failed for %s", input.App)
	}
	return usecase.DeployResult{Targets: d.targets}, nil
}

// ConfigNodes returns preview node metadata.
func (d *previewDeployer) ConfigNodes(_ context.Context, _ usecase.RuntimeConfigInput) ([]usecase.ConfigNode, error) {
	return []usecase.ConfigNode{
		{ID: "node-a", Host: "100.64.0.10", PrivateIP: "100.64.0.10"},
		{ID: "node-b", Host: "100.64.0.11", PrivateIP: "100.64.0.11"},
	}, nil
}

// RuntimeStatus returns preview cluster reachability.
func (d *previewDeployer) RuntimeStatus(_ context.Context, _ usecase.RuntimeConfigInput) (usecase.RuntimeStatus, error) {
	return usecase.RuntimeStatus{
		Nodes: []usecase.RuntimeNode{
			{ID: "node-a", Host: "100.64.0.10", PrivateIP: "100.64.0.10", Reachable: true},
			{ID: "node-b", Host: "100.64.0.11", PrivateIP: "100.64.0.11", Reachable: true},
		},
	}, nil
}

// AppRuntimeStatus returns preview app status rows.
func (d *previewDeployer) AppRuntimeStatus(_ context.Context, _ usecase.RuntimeConfigInput, app string) ([]usecase.RuntimeNodeStatus, error) {
	return []usecase.RuntimeNodeStatus{
		{NodeID: "node-a", State: "running", Version: app + "-preview", Slot: "blue"},
		{NodeID: "node-b", State: "running", Version: app + "-preview", Slot: "blue"},
	}, nil
}

// Logs returns preview log output.
func (d *previewDeployer) Logs(_ context.Context, _ usecase.RuntimeConfigInput, input usecase.LogsInput) (usecase.LogsResult, error) {
	app := input.App
	if app == "" {
		app = "api"
	}
	return usecase.LogsResult{
		Output: "[" + app + "] preview log line\n[" + app + "] ready\n",
	}, nil
}

const previewClusterYAML = `version: "2"
project: preview

nodes:
  - id: node-a
    host: 100.64.0.10
    private_ip: 100.64.0.10
  - id: node-b
    host: 100.64.0.11
    private_ip: 100.64.0.11
`

const previewAPIAppYAML = `kind: warpgate/app
release:
  services:
    api:
      image: ghcr.io/acme/api
      image_tag: 1.2.0
      image_semver: "~1.2"
      port: 8080
      expose:
        public:
          domains: [api.preview.example.com]
`

const previewWebAppYAML = `kind: warpgate/app
release:
  services:
    web:
      image: ghcr.io/acme/web
      image_tag: 3.4.1
      port: 3000
      expose:
        public:
          domains: [preview.example.com]
`

const previewAPIComposeYAML = `services:
  api:
    image: ghcr.io/acme/api:1.2.0
    ports:
      - "8080:8080"
`

const previewWebComposeYAML = `services:
  web:
    image: ghcr.io/acme/web:3.4.1
    ports:
      - "3000:3000"
`
