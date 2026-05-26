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

type fakeRegistry struct{}

// ResolveDigest returns an empty fake digest.
func (fakeRegistry) ResolveDigest(context.Context, string, string) (string, error) {
	return "", nil
}

type fakeDeployer struct{}

// DeployRelease returns an empty fake deployment result.
func (fakeDeployer) DeployRelease(context.Context, usecase.DeployReleaseInput) (usecase.DeployResult, error) {
	return usecase.DeployResult{}, nil
}
