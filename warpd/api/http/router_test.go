package http

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/usecase"
)

func TestAttachRepositoryErrorRendersSettingsPage(t *testing.T) {
	service := usecase.NewService(
		turso.NewMemoryStore(),
		failingGitHub{err: errors.New("GitHub repository or branch not found: pangobit/brighter@master. If this is a private repository, authorize the GitHub App and install it for this repository.")},
		fakeRegistry{},
		fakeDeployer{},
	)
	router := NewRouter(service, identity.StaticIdentifier{User: identity.User{
		Email:        "unknown",
		DisplayName:  "unknown",
		Capabilities: []string{identity.AdminCapability},
	}}, nethttp.NotFoundHandler())

	form := url.Values{}
	form.Set("owner", "pangobit")
	form.Set("repo", "brighter")
	form.Set("branch", "master")
	form.Set("path", "prod")
	req := httptest.NewRequest(nethttp.MethodPost, "/settings/repository", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.Code, nethttp.StatusBadRequest)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "<title>Settings</title>") {
		t.Fatalf("expected settings page, got %q", body)
	}
	if !strings.Contains(body, "pangobit/brighter@master") {
		t.Fatalf("expected inline repo error, got %q", body)
	}
	if !strings.Contains(body, `name="branch" value="master"`) {
		t.Fatalf("expected submitted branch to be preserved")
	}
	if !strings.Contains(body, `name="path" value="prod"`) {
		t.Fatalf("expected submitted path to be preserved")
	}
}

func TestStatusRouteRendersRuntimeStatus(t *testing.T) {
	store := turso.NewMemoryStore()
	seedRuntimeConfig(t, store)
	service := usecase.NewService(
		store,
		failingGitHub{err: errors.New("unused")},
		fakeRegistry{},
		fakeDeployer{runtime: usecase.RuntimeStatus{
			Nodes: []usecase.RuntimeNode{{ID: "node-1", Host: "10.0.0.1", Reachable: true}},
			Apps:  []usecase.RuntimeAppStatus{{App: "api", NodeID: "node-1", State: "healthy"}},
		}},
	)
	router := NewRouter(service, identity.StaticIdentifier{User: adminUser()}, nethttp.NotFoundHandler())
	req := httptest.NewRequest(nethttp.MethodGet, "/status", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, nethttp.StatusOK)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "node-1") || !strings.Contains(body, "healthy") {
		t.Fatalf("expected runtime status, got %q", body)
	}
}

func TestLogsRouteRendersLogOutput(t *testing.T) {
	store := turso.NewMemoryStore()
	seedRuntimeConfig(t, store)
	service := usecase.NewService(
		store,
		failingGitHub{err: errors.New("unused")},
		fakeRegistry{},
		fakeDeployer{
			nodes: []usecase.ConfigNode{{ID: "node-1", Host: "10.0.0.1"}},
			logs:  usecase.LogsResult{Output: "[api] ready\n"},
		},
	)
	router := NewRouter(service, identity.StaticIdentifier{User: adminUser()}, nethttp.NotFoundHandler())
	req := httptest.NewRequest(nethttp.MethodGet, "/logs?node=node-1&tail=25&grep=ready", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, nethttp.StatusOK)
	}
	body := resp.Body.String()
	if !strings.Contains(body, ">api<") || !strings.Contains(body, ">ready<") {
		t.Fatalf("expected log output, got %q", body)
	}
}

func TestDeployDataChangesFromFormReadsAllServices(t *testing.T) {
	form := url.Values{}
	form.Add("service", "api")
	form.Add("image_tag", "v2.0.0")
	form.Add("image_digest", "")
	form.Add("service", "worker")
	form.Add("image_tag", "v2.0.1")
	form.Add("image_digest", "sha256:worker")
	req := httptest.NewRequest(nethttp.MethodPost, "/apps/api/commit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm() error = %v", err)
	}

	changes := deployDataChangesFromForm(req)

	if len(changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(changes))
	}
	if changes[0].Service != "api" || changes[0].ImageTag != "v2.0.0" {
		t.Fatalf("first change = %+v", changes[0])
	}
	if changes[1].Service != "worker" || changes[1].ImageDigest != "sha256:worker" {
		t.Fatalf("second change = %+v", changes[1])
	}
}

func TestDeployReleaseErrorRendersReleasePage(t *testing.T) {
	store := turso.NewMemoryStore()
	seedRuntimeConfig(t, store)
	if err := store.CreateRelease(context.Background(), release.Record{
		ID:           "rel-1",
		App:          "api",
		ConfigCommit: "commit-1",
		Status:       release.StatusReady,
	}); err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	service := usecase.NewService(
		store,
		failingGitHub{err: errors.New("unused")},
		fakeRegistry{},
		fakeDeployer{err: errors.New("invalid cluster config")},
	)
	router := NewRouter(service, identity.StaticIdentifier{User: adminUser()}, nethttp.NotFoundHandler())
	req := httptest.NewRequest(nethttp.MethodPost, "/releases/rel-1/deploy", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.Code, nethttp.StatusBadRequest)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "<title>Release rel-1</title>") {
		t.Fatalf("expected release page, got %q", body)
	}
	if !strings.Contains(body, "invalid cluster config") {
		t.Fatalf("expected inline deploy error, got %q", body)
	}
}

func TestStartGitHubAuthStoresClientIDCookie(t *testing.T) {
	service := usecase.NewService(turso.NewMemoryStore(), failingGitHub{err: errors.New("unused")}, fakeRegistry{}, fakeDeployer{})
	auth := &fakeGitHubAuth{}
	router := NewRouter(
		service,
		identity.StaticIdentifier{User: adminUser()},
		nethttp.NotFoundHandler(),
		WithGitHubAuth(auth),
	)
	form := url.Values{}
	form.Set("github_client_id", "Iv1.client-id")
	req := httptest.NewRequest(nethttp.MethodPost, "/auth/github/start", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != nethttp.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.Code, nethttp.StatusSeeOther)
	}
	if auth.clientID != "Iv1.client-id" || !auth.started {
		t.Fatalf("auth clientID = %q started = %v", auth.clientID, auth.started)
	}
	cookies := resp.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != githubClientIDCookieName || cookies[0].Value != "Iv1.client-id" {
		t.Fatalf("cookies = %+v", cookies)
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != nethttp.SameSiteStrictMode {
		t.Fatalf("cookie security flags = %+v", cookies[0])
	}
}

func TestSettingsAppliesGitHubClientIDCookie(t *testing.T) {
	service := usecase.NewService(turso.NewMemoryStore(), failingGitHub{err: errors.New("unused")}, fakeRegistry{}, fakeDeployer{})
	auth := &fakeGitHubAuth{}
	router := NewRouter(
		service,
		identity.StaticIdentifier{User: adminUser()},
		nethttp.NotFoundHandler(),
		WithGitHubAuth(auth),
	)
	req := httptest.NewRequest(nethttp.MethodGet, "/settings", nil)
	req.AddCookie(&nethttp.Cookie{Name: githubClientIDCookieName, Value: "Iv1.from-cookie"})
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, nethttp.StatusOK)
	}
	if auth.clientID != "Iv1.from-cookie" {
		t.Fatalf("auth clientID = %q, want cookie value", auth.clientID)
	}
	if !strings.Contains(resp.Body.String(), `name="github_client_id" value="Iv1.from-cookie"`) {
		t.Fatalf("expected settings form to use cookie client ID")
	}
}

type failingGitHub struct {
	err error
}

// BranchHead returns the configured failure.
func (f failingGitHub) BranchHead(context.Context, configrepo.RepositorySettings) (string, error) {
	return "", f.err
}

// ReadFile returns the configured failure.
func (f failingGitHub) ReadFile(context.Context, configrepo.RepositorySettings, string, string) (usecase.GitHubFile, error) {
	return usecase.GitHubFile{}, f.err
}

// ListAppConfigFiles returns the configured failure.
func (f failingGitHub) ListAppConfigFiles(context.Context, configrepo.RepositorySettings, string) ([]usecase.GitHubFile, error) {
	return nil, f.err
}

// WriteFile returns the configured failure.
func (f failingGitHub) WriteFile(context.Context, usecase.WriteFileInput) (usecase.GitHubFile, error) {
	return usecase.GitHubFile{}, f.err
}

// WriteFiles returns the configured failure.
func (f failingGitHub) WriteFiles(context.Context, usecase.WriteFilesInput) ([]usecase.GitHubFile, error) {
	return nil, f.err
}

type fakeRegistry struct{}

// ResolveDigest returns an empty fake digest.
func (fakeRegistry) ResolveDigest(context.Context, string, string) (string, error) {
	return "", nil
}

type fakeDeployer struct {
	nodes   []usecase.ConfigNode
	runtime usecase.RuntimeStatus
	logs    usecase.LogsResult
	err     error
}

// DeployRelease returns an empty fake deployment result.
func (f fakeDeployer) DeployRelease(context.Context, usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	return usecase.DeployResult{}, f.err
}

// ConfigNodes returns fake config nodes.
func (f fakeDeployer) ConfigNodes(context.Context, usecase.RuntimeConfigInput) ([]usecase.ConfigNode, error) {
	return f.nodes, f.err
}

// RuntimeStatus returns fake runtime status.
func (f fakeDeployer) RuntimeStatus(context.Context, usecase.RuntimeConfigInput) (usecase.RuntimeStatus, error) {
	return f.runtime, f.err
}

// AppRuntimeStatus returns empty fake app runtime status.
func (f fakeDeployer) AppRuntimeStatus(context.Context, usecase.RuntimeConfigInput, string) ([]usecase.RuntimeNodeStatus, error) {
	return nil, f.err
}

// Logs returns fake logs.
func (f fakeDeployer) Logs(context.Context, usecase.RuntimeConfigInput, usecase.LogsInput) (usecase.LogsResult, error) {
	return f.logs, f.err
}

type fakeGitHubAuth struct {
	clientID string
	started  bool
}

// CompleteDeviceFlow marks the fake device flow complete.
func (f *fakeGitHubAuth) CompleteDeviceFlow(context.Context) error {
	return nil
}

// Disconnect clears the fake authorization.
func (f *fakeGitHubAuth) Disconnect() {}

// SetClientID records the configured client ID.
func (f *fakeGitHubAuth) SetClientID(clientID string) {
	f.clientID = clientID
}

// StartDeviceFlow records that the fake device flow started.
func (f *fakeGitHubAuth) StartDeviceFlow(context.Context) error {
	f.started = true
	return nil
}

// Status returns the fake authorization status.
func (f *fakeGitHubAuth) Status() identity.GitHubAuthStatus {
	return identity.GitHubAuthStatus{Configured: f.clientID != "", ClientID: f.clientID}
}

func adminUser() identity.User {
	return identity.User{
		Email:        "admin@example.com",
		DisplayName:  "Admin",
		Capabilities: []string{identity.AdminCapability},
	}
}

func seedRuntimeConfig(t *testing.T, store *turso.MemoryStore) {
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
		RawYAML: "version: \"2\"\nproject: acme\nnodes:\n  - id: node-1\n    host: 10.0.0.1\n    private_ip: 100.95.30.69\nnetworking:\n  private_network: example.ts.net\n  dns:\n    provider: cloudflare\n    zone: example.com\n  traefik:\n    entry_points: [web]\nregistry:\n  server: ghcr.io\n",
	}); err != nil {
		t.Fatalf("SaveClusterConfig() error = %v", err)
	}
}
