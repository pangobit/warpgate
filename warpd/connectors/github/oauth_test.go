package github

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
)

func TestDeviceSessionDeviceFlow(t *testing.T) {
	store := &memorySessionStore{}
	session := NewDeviceSession("client-id", store)
	session.now = func() time.Time { return time.Unix(100, 0).UTC() }
	session.oauthURL = "https://github.test"
	session.apiURL = "https://api.github.test"
	session.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login/device/code":
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			if r.Form.Get("client_id") != "client-id" {
				t.Fatalf("client_id = %q", r.Form.Get("client_id"))
			}
			if r.Form.Get("scope") != "" {
				t.Fatalf("scope = %q", r.Form.Get("scope"))
			}
			return jsonResponse(t, map[string]any{
				"device_code":      "device-code",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://github.com/login/device",
				"expires_in":       900,
				"interval":         5,
			})
		case "/login/oauth/access_token":
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			if r.Form.Get("device_code") != "device-code" {
				t.Fatalf("device_code = %q", r.Form.Get("device_code"))
			}
			return jsonResponse(t, map[string]any{
				"access_token":             "access-token",
				"expires_in":               28800,
				"refresh_token":            "refresh-token",
				"refresh_token_expires_in": 15897600,
				"token_type":               "bearer",
			})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			return jsonResponse(t, map[string]any{
				"login": "octo",
				"name":  "Octo User",
			})
		default:
			return jsonResponse(t, map[string]any{"error": "not found"}, http.StatusNotFound)
		}
	})}

	if err := session.StartDeviceFlow(context.Background()); err != nil {
		t.Fatalf("start device flow: %v", err)
	}
	status := session.Status()
	if !status.Configured || status.UserCode != "ABCD-1234" {
		t.Fatalf("status after start = %+v", status)
	}

	if err := session.CompleteDeviceFlow(context.Background()); err != nil {
		t.Fatalf("complete device flow: %v", err)
	}
	status = session.Status()
	if !status.Authenticated || status.DisplayName != "Octo User" {
		t.Fatalf("status after complete = %+v", status)
	}
	token, err := session.Token(context.Background())
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if token != "access-token" {
		t.Fatalf("token = %q", token)
	}
	saved, ok, err := store.GitHubSession(context.Background())
	if err != nil || !ok {
		t.Fatalf("GitHubSession() ok = %v error = %v", ok, err)
	}
	if saved.AccessToken != "access-token" || saved.RefreshToken != "refresh-token" {
		t.Fatalf("saved tokens = %q / %q", saved.AccessToken, saved.RefreshToken)
	}
	if saved.AccessTokenExpiresAt.IsZero() || saved.RefreshTokenExpiresAt.IsZero() {
		t.Fatalf("expected token expirations to be saved")
	}
}

func TestDeviceSessionLoadsPersistedAuthorization(t *testing.T) {
	store := &memorySessionStore{
		session: identity.GitHubSession{
			AccessToken: "persisted-token",
			User: identity.User{
				Email:        "octo",
				DisplayName:  "Octo User",
				Capabilities: []string{identity.AdminCapability},
			},
		},
		ok: true,
	}
	session := NewDeviceSession("client-id", store)
	if err := session.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	status := session.Status()
	if !status.Authenticated || status.DisplayName != "Octo User" {
		t.Fatalf("status = %+v", status)
	}
	token, err := session.Token(context.Background())
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if token != "persisted-token" {
		t.Fatalf("token = %q", token)
	}
}

func TestDeviceSessionRefreshesExpiredAuthorization(t *testing.T) {
	store := &memorySessionStore{
		session: identity.GitHubSession{
			AccessToken:           "expired-token",
			AccessTokenExpiresAt:  time.Unix(100, 0).UTC(),
			RefreshToken:          "refresh-token",
			RefreshTokenExpiresAt: time.Unix(1000, 0).UTC(),
			User: identity.User{
				Email:        "octo",
				DisplayName:  "Octo User",
				Capabilities: []string{identity.AdminCapability},
			},
		},
		ok: true,
	}
	session := NewDeviceSession("client-id", store)
	session.now = func() time.Time { return time.Unix(200, 0).UTC() }
	session.oauthURL = "https://github.test"
	session.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/login/oauth/access_token" {
			return jsonResponse(t, map[string]any{"error": "not found"}, http.StatusNotFound)
		}
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "refresh-token" {
			t.Fatalf("refresh_token = %q", r.Form.Get("refresh_token"))
		}
		return jsonResponse(t, map[string]any{
			"access_token":             "fresh-token",
			"expires_in":               28800,
			"refresh_token":            "fresh-refresh-token",
			"refresh_token_expires_in": 15897600,
			"token_type":               "bearer",
		})
	})}
	if err := session.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	token, err := session.Token(context.Background())
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if token != "fresh-token" {
		t.Fatalf("token = %q", token)
	}
	saved, ok, err := store.GitHubSession(context.Background())
	if err != nil || !ok {
		t.Fatalf("GitHubSession() ok = %v error = %v", ok, err)
	}
	if saved.AccessToken != "fresh-token" || saved.RefreshToken != "fresh-refresh-token" {
		t.Fatalf("saved tokens = %q / %q", saved.AccessToken, saved.RefreshToken)
	}
}

func TestDeviceSessionDisconnectDeletesPersistedAuthorization(t *testing.T) {
	store := &memorySessionStore{
		session: identity.GitHubSession{AccessToken: "persisted-token"},
		ok:      true,
	}
	session := NewDeviceSession("client-id", store)
	if err := session.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	session.Disconnect()
	if _, ok, err := store.GitHubSession(context.Background()); err != nil || ok {
		t.Fatalf("GitHubSession() ok = %v error = %v", ok, err)
	}
	if session.Status().Authenticated {
		t.Fatalf("expected disconnected status")
	}
}

func TestDeviceSessionClearsExpiredAuthorizationWithoutRefresh(t *testing.T) {
	store := &memorySessionStore{
		session: identity.GitHubSession{
			AccessToken:          "expired-token",
			AccessTokenExpiresAt: time.Unix(100, 0).UTC(),
			User: identity.User{
				Email:        "octo",
				DisplayName:  "Octo User",
				Capabilities: []string{identity.AdminCapability},
			},
		},
		ok: true,
	}
	session := NewDeviceSession("client-id", store)
	session.now = func() time.Time { return time.Unix(200, 0).UTC() }
	if err := session.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err := session.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Token() error = %v", err)
	}
	if _, ok, err := store.GitHubSession(context.Background()); err != nil || ok {
		t.Fatalf("GitHubSession() ok = %v error = %v", ok, err)
	}
}

func TestDeviceSessionReportsPendingAuthorization(t *testing.T) {
	session := NewDeviceSession("client-id")
	session.oauthURL = "https://github.test"
	session.apiURL = "https://api.github.test"
	session.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login/device/code":
			return jsonResponse(t, map[string]any{
				"device_code":      "device-code",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://github.com/login/device",
				"expires_in":       900,
			})
		case "/login/oauth/access_token":
			return jsonResponse(t, map[string]any{
				"error":             "authorization_pending",
				"error_description": "pending",
			})
		default:
			return jsonResponse(t, map[string]any{"error": "not found"}, http.StatusNotFound)
		}
	})}

	if err := session.StartDeviceFlow(context.Background()); err != nil {
		t.Fatalf("start device flow: %v", err)
	}
	err := session.CompleteDeviceFlow(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("complete error = %v", err)
	}
	if session.Status().Error == "" {
		t.Fatalf("expected pending authorization to be visible in status")
	}
}

func TestBranchHeadNotFoundIsActionable(t *testing.T) {
	client := NewClientWithTokenProvider(staticTokenProvider("token"))
	client.baseURL = "https://api.github.test"
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/pangobit/brighter/branches/master":
			return jsonResponse(t, map[string]any{
				"message":           "Not Found",
				"documentation_url": "https://docs.github.com/rest/branches/branches#get-a-branch",
				"status":            "404",
			}, http.StatusNotFound)
		case "/user/installations":
			return jsonResponse(t, map[string]any{
				"installations": []map[string]any{
					{
						"id":                   12,
						"repository_selection": "selected",
						"account":              map[string]any{"login": "pangobit"},
						"permissions":          map[string]any{"contents": "read"},
					},
				},
			})
		case "/user/installations/12/repositories":
			return jsonResponse(t, map[string]any{
				"repositories": []map[string]any{
					{"full_name": "pangobit/another-repo"},
				},
			})
		default:
			return jsonResponse(t, map[string]any{
				"message": "Not Found",
			}, http.StatusNotFound)
		}
	})}

	_, err := client.BranchHead(context.Background(), configrepo.RepositorySettings{
		Owner:  "pangobit",
		Repo:   "brighter",
		Branch: "master",
	})
	if err == nil {
		t.Fatalf("expected branch head error")
	}
	message := err.Error()
	if !strings.Contains(message, "pangobit/brighter") {
		t.Fatalf("expected actionable repo in error, got %q", message)
	}
	if !strings.Contains(message, "not selected") {
		t.Fatalf("expected GitHub App installation guidance, got %q", message)
	}
	if strings.Contains(message, "documentation_url") {
		t.Fatalf("expected sanitized GitHub error, got %q", message)
	}
}

func TestBranchHeadNotFoundFallsBackWhenAppCanAccessRepository(t *testing.T) {
	client := NewClientWithTokenProvider(staticTokenProvider("token"))
	client.baseURL = "https://api.github.test"
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/pangobit/brighter/branches/master":
			return jsonResponse(t, map[string]any{
				"message": "Not Found",
			}, http.StatusNotFound)
		case "/user/installations":
			return jsonResponse(t, map[string]any{
				"installations": []map[string]any{
					{
						"id":                   12,
						"repository_selection": "selected",
						"account":              map[string]any{"login": "pangobit"},
						"permissions":          map[string]any{"contents": "read"},
					},
				},
			})
		case "/user/installations/12/repositories":
			return jsonResponse(t, map[string]any{
				"repositories": []map[string]any{
					{"full_name": "pangobit/brighter"},
				},
			})
		default:
			return jsonResponse(t, map[string]any{
				"message": "Not Found",
			}, http.StatusNotFound)
		}
	})}

	_, err := client.BranchHead(context.Background(), configrepo.RepositorySettings{
		Owner:  "pangobit",
		Repo:   "brighter",
		Branch: "master",
	})
	if err == nil {
		t.Fatalf("expected branch head error")
	}
	message := err.Error()
	if !strings.Contains(message, "pangobit/brighter@master") {
		t.Fatalf("expected repo and branch in error, got %q", message)
	}
	if strings.Contains(message, "not selected") {
		t.Fatalf("expected missing branch fallback, got %q", message)
	}
}

func TestBranchHeadReportsMissingContentsPermission(t *testing.T) {
	client := NewClientWithTokenProvider(staticTokenProvider("token"))
	client.baseURL = "https://api.github.test"
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/pangobit/brighter/branches/master":
			return jsonResponse(t, map[string]any{
				"message": "Not Found",
			}, http.StatusNotFound)
		case "/user/installations":
			return jsonResponse(t, map[string]any{
				"installations": []map[string]any{
					{
						"id":                   12,
						"repository_selection": "selected",
						"account":              map[string]any{"login": "pangobit"},
						"permissions":          map[string]any{"metadata": "read"},
					},
				},
			})
		case "/user/installations/12/repositories":
			return jsonResponse(t, map[string]any{
				"repositories": []map[string]any{
					{"full_name": "pangobit/brighter"},
				},
			})
		default:
			return jsonResponse(t, map[string]any{
				"message": "Not Found",
			}, http.StatusNotFound)
		}
	})}

	_, err := client.BranchHead(context.Background(), configrepo.RepositorySettings{
		Owner:  "pangobit",
		Repo:   "brighter",
		Branch: "master",
	})
	if err == nil {
		t.Fatalf("expected branch head error")
	}
	if !strings.Contains(err.Error(), "Contents: read") {
		t.Fatalf("expected contents permission guidance, got %q", err.Error())
	}
}

func TestBranchHeadNotFoundWithoutTokenProviderKeepsGenericMessage(t *testing.T) {
	client := NewClient()
	client.baseURL = "https://api.github.test"
	client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(t, map[string]any{
			"message":           "Not Found",
			"documentation_url": "https://docs.github.com/rest/branches/branches#get-a-branch",
			"status":            "404",
		}, http.StatusNotFound)
	})}

	_, err := client.BranchHead(context.Background(), configrepo.RepositorySettings{
		Owner:  "pangobit",
		Repo:   "brighter",
		Branch: "master",
	})
	if err == nil {
		t.Fatalf("expected branch head error")
	}
	message := err.Error()
	if !strings.Contains(message, "pangobit/brighter@master") {
		t.Fatalf("expected actionable repo and branch in error, got %q", message)
	}
	if strings.Contains(message, "documentation_url") {
		t.Fatalf("expected sanitized GitHub error, got %q", message)
	}
}

func TestAppConfigPathUsesRepositorySubpath(t *testing.T) {
	root := repositoryPathPrefix(configrepo.RepositorySettings{Path: "/prod/"})
	cases := []struct {
		path string
		want bool
	}{
		{path: "prod/apps/api/app.yml", want: true},
		{path: "apps/api/app.yml", want: false},
		{path: "prod/apps/api/compose.yml", want: false},
		{path: "prod/other/api/app.yml", want: false},
	}
	for _, tt := range cases {
		if got := isAppConfigPath(root, tt.path); got != tt.want {
			t.Fatalf("isAppConfigPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

type staticTokenProvider string

// Token returns the static token value.
func (s staticTokenProvider) Token(context.Context) (string, error) {
	return string(s), nil
}

type memorySessionStore struct {
	session identity.GitHubSession
	ok      bool
}

// GitHubSession returns the stored test session.
func (s *memorySessionStore) GitHubSession(context.Context) (identity.GitHubSession, bool, error) {
	return s.session, s.ok, nil
}

// SaveGitHubSession stores the test session.
func (s *memorySessionStore) SaveGitHubSession(_ context.Context, session identity.GitHubSession) error {
	s.session = session
	s.ok = true
	return nil
}

// DeleteGitHubSession removes the test session.
func (s *memorySessionStore) DeleteGitHubSession(context.Context) error {
	s.session = identity.GitHubSession{}
	s.ok = false
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip executes the fake transport function.
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(t *testing.T, value any, status ...int) (*http.Response, error) {
	t.Helper()
	code := http.StatusOK
	if len(status) > 0 {
		code = status[0]
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		t.Fatalf("write json: %v", err)
	}
	return &http.Response{
		StatusCode: code,
		Status:     http.StatusText(code),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(&body),
	}, nil
}
