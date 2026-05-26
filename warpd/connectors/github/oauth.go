package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/identity"
)

const (
	defaultGitHubWebURL = "https://github.com"
)

// ErrAuthorizationPending indicates that GitHub has not approved the device flow yet.
var ErrAuthorizationPending = errors.New("github authorization is still pending")

// DeviceSession manages one local GitHub device-flow session.
type DeviceSession struct {
	mu sync.Mutex

	clientID   string
	oauthURL   string
	apiURL     string
	httpClient *http.Client

	pending     deviceFlow
	accessToken string
	user        identity.User
	lastError   string
}

type deviceFlow struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	Interval        time.Duration
}

// NewDeviceSession creates a local GitHub device-flow session.
func NewDeviceSession(clientID string) *DeviceSession {
	return &DeviceSession{
		clientID: clientID,
		oauthURL: defaultGitHubWebURL,
		apiURL:   defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Status returns the current GitHub authorization state.
func (s *DeviceSession) Status() identity.GitHubAuthStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := identity.GitHubAuthStatus{
		Configured:    s.clientID != "",
		Authenticated: s.accessToken != "",
		Login:         s.user.Email,
		DisplayName:   s.user.DisplayName,
		Error:         s.lastError,
	}
	if s.pending.DeviceCode != "" && time.Now().Before(s.pending.ExpiresAt) {
		status.UserCode = s.pending.UserCode
		status.VerificationURI = s.pending.VerificationURI
	}
	return status
}

// Identify returns the signed-in GitHub user or a local unknown identity.
func (s *DeviceSession) Identify(_ context.Context, _ string) (identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.user.Email != "" {
		return s.user, nil
	}
	return identity.User{
		Email:        "unknown",
		DisplayName:  "unknown",
		Capabilities: []string{identity.AdminCapability},
	}, nil
}

// Token returns the current GitHub access token.
func (s *DeviceSession) Token(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accessToken == "" {
		return "", fmt.Errorf("connect GitHub before using this repository")
	}
	return s.accessToken, nil
}

// StartDeviceFlow starts GitHub's device authorization flow.
func (s *DeviceSession) StartDeviceFlow(ctx context.Context) error {
	if s.clientID == "" {
		return s.setError(fmt.Errorf("github client id is required"))
	}
	var response struct {
		DeviceCode       string `json:"device_code"`
		UserCode         string `json:"user_code"`
		VerificationURI  string `json:"verification_uri"`
		ExpiresIn        int    `json:"expires_in"`
		Interval         int    `json:"interval"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	values := url.Values{}
	values.Set("client_id", s.clientID)
	if err := s.postForm(ctx, s.oauthURL+"/login/device/code", values, &response); err != nil {
		return s.setError(err)
	}
	if response.Error != "" {
		return s.setError(githubAuthorizationError(response.Error, response.ErrorDescription))
	}
	if response.DeviceCode == "" || response.UserCode == "" || response.VerificationURI == "" {
		return s.setError(fmt.Errorf("github device flow returned an incomplete response"))
	}
	interval := time.Duration(response.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	s.mu.Lock()
	s.pending = deviceFlow{
		DeviceCode:      response.DeviceCode,
		UserCode:        response.UserCode,
		VerificationURI: response.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
		Interval:        interval,
	}
	s.lastError = ""
	s.mu.Unlock()
	return nil
}

// CompleteDeviceFlow exchanges an approved device flow for a GitHub token.
func (s *DeviceSession) CompleteDeviceFlow(ctx context.Context) error {
	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()
	if pending.DeviceCode == "" {
		return s.setError(fmt.Errorf("start GitHub authorization first"))
	}
	if time.Now().After(pending.ExpiresAt) {
		return s.setError(fmt.Errorf("GitHub authorization code expired"))
	}
	var tokenResponse struct {
		AccessToken      string `json:"access_token"`
		TokenType        string `json:"token_type"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	values := url.Values{}
	values.Set("client_id", s.clientID)
	values.Set("device_code", pending.DeviceCode)
	values.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	if err := s.postForm(ctx, s.oauthURL+"/login/oauth/access_token", values, &tokenResponse); err != nil {
		return s.setError(err)
	}
	if tokenResponse.Error == "authorization_pending" {
		return s.setError(ErrAuthorizationPending)
	}
	if tokenResponse.Error != "" {
		return s.setError(githubAuthorizationError(tokenResponse.Error, tokenResponse.ErrorDescription))
	}
	if tokenResponse.AccessToken == "" {
		return s.setError(fmt.Errorf("GitHub did not return an access token"))
	}
	user, err := s.fetchUser(ctx, tokenResponse.AccessToken)
	if err != nil {
		return s.setError(err)
	}
	s.mu.Lock()
	s.accessToken = tokenResponse.AccessToken
	s.user = user
	s.pending = deviceFlow{}
	s.lastError = ""
	s.mu.Unlock()
	return nil
}

// Disconnect clears the local GitHub session.
func (s *DeviceSession) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = deviceFlow{}
	s.accessToken = ""
	s.user = identity.User{}
	s.lastError = ""
}

func (s *DeviceSession) postForm(ctx context.Context, endpoint string, values url.Values, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if readErr != nil {
			return fmt.Errorf("github authorization request failed: %s", resp.Status)
		}
		return fmt.Errorf("github authorization request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *DeviceSession) fetchUser(ctx context.Context, token string) (identity.User, error) {
	var response struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL+"/user", nil)
	if err != nil {
		return identity.User{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return identity.User{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if readErr != nil {
			return identity.User{}, fmt.Errorf("github user request failed: %s", resp.Status)
		}
		return identity.User{}, fmt.Errorf("github user request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return identity.User{}, err
	}
	if response.Login == "" {
		return identity.User{}, fmt.Errorf("github user response did not include login")
	}
	displayName := response.Name
	if displayName == "" {
		displayName = response.Login
	}
	return identity.User{
		Email:        response.Login,
		DisplayName:  displayName,
		Capabilities: []string{identity.AdminCapability},
	}, nil
}

func (s *DeviceSession) setError(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err.Error()
	return err
}

func githubAuthorizationError(code string, description string) error {
	if description == "" {
		return fmt.Errorf("github authorization error: %s", code)
	}
	return fmt.Errorf("github authorization error: %s: %s", code, description)
}
