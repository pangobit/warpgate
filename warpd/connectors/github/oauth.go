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
	tokenRefreshSkew    = time.Minute
)

// ErrAuthorizationPending indicates that GitHub has not approved the device flow yet.
var ErrAuthorizationPending = errors.New("github authorization is still pending")

// SessionStore persists GitHub authorization between local UI runs.
type SessionStore interface {
	DeleteGitHubSession(ctx context.Context) error
	GitHubSession(ctx context.Context) (identity.GitHubSession, bool, error)
	SaveGitHubSession(ctx context.Context, session identity.GitHubSession) error
}

// DeviceSession manages one local GitHub device-flow session.
type DeviceSession struct {
	mu sync.Mutex

	clientID   string
	oauthURL   string
	apiURL     string
	httpClient *http.Client
	store      SessionStore
	now        func() time.Time

	pending               deviceFlow
	accessToken           string
	accessTokenExpiresAt  time.Time
	refreshToken          string
	refreshTokenExpiresAt time.Time
	tokenType             string
	user                  identity.User
	lastError             string
}

type deviceFlow struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	Interval        time.Duration
}

// NewDeviceSession creates a local GitHub device-flow session.
func NewDeviceSession(clientID string, stores ...SessionStore) *DeviceSession {
	var store SessionStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return &DeviceSession{
		clientID: clientID,
		oauthURL: defaultGitHubWebURL,
		apiURL:   defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		store: store,
		now:   time.Now,
	}
}

// Load restores a persisted GitHub authorization.
func (s *DeviceSession) Load(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	session, ok, err := s.store.GitHubSession(ctx)
	if err != nil {
		return err
	}
	if !ok || session.AccessToken == "" {
		return nil
	}
	s.mu.Lock()
	s.applySessionLocked(session)
	s.lastError = ""
	s.mu.Unlock()
	return nil
}

// Status returns the current GitHub authorization state.
func (s *DeviceSession) Status() identity.GitHubAuthStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := identity.GitHubAuthStatus{
		Configured:    s.clientID != "",
		Authenticated: s.usableLocked(),
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
func (s *DeviceSession) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.accessToken == "" {
		s.mu.Unlock()
		return "", fmt.Errorf("connect GitHub before using this repository")
	}
	if !s.accessExpiredLocked() {
		token := s.accessToken
		s.mu.Unlock()
		return token, nil
	}
	if s.refreshToken == "" || s.refreshExpiredLocked() {
		s.clearLocked()
		s.mu.Unlock()
		if err := s.deletePersistedSession(ctx); err != nil {
			return "", err
		}
		return "", fmt.Errorf("GitHub authorization expired; reconnect GitHub")
	}
	refreshToken := s.refreshToken
	s.mu.Unlock()
	session, err := s.refreshSession(ctx, refreshToken)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastError = err.Error()
		return "", err
	}
	s.applySessionLocked(session)
	if err := s.savePersistedSession(ctx, session); err != nil {
		return "", err
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
		AccessToken           string `json:"access_token"`
		ExpiresIn             int    `json:"expires_in"`
		RefreshToken          string `json:"refresh_token"`
		RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
		TokenType             string `json:"token_type"`
		Error                 string `json:"error"`
		ErrorDescription      string `json:"error_description"`
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
	session := s.sessionFromTokenResponse(tokenResponse.AccessToken, tokenResponse.TokenType, tokenResponse.ExpiresIn, tokenResponse.RefreshToken, tokenResponse.RefreshTokenExpiresIn, user)
	if err := s.savePersistedSession(ctx, session); err != nil {
		return s.setError(err)
	}
	s.mu.Lock()
	s.applySessionLocked(session)
	s.pending = deviceFlow{}
	s.lastError = ""
	s.mu.Unlock()
	return nil
}

// Disconnect clears the local GitHub session.
func (s *DeviceSession) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
	if err := s.deletePersistedSession(context.Background()); err != nil {
		s.lastError = err.Error()
	}
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

func (s *DeviceSession) refreshSession(ctx context.Context, refreshToken string) (identity.GitHubSession, error) {
	var tokenResponse struct {
		AccessToken           string `json:"access_token"`
		ExpiresIn             int    `json:"expires_in"`
		RefreshToken          string `json:"refresh_token"`
		RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
		TokenType             string `json:"token_type"`
		Error                 string `json:"error"`
		ErrorDescription      string `json:"error_description"`
	}
	values := url.Values{}
	values.Set("client_id", s.clientID)
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	if err := s.postForm(ctx, s.oauthURL+"/login/oauth/access_token", values, &tokenResponse); err != nil {
		return identity.GitHubSession{}, err
	}
	if tokenResponse.Error != "" {
		return identity.GitHubSession{}, githubAuthorizationError(tokenResponse.Error, tokenResponse.ErrorDescription)
	}
	if tokenResponse.AccessToken == "" {
		return identity.GitHubSession{}, fmt.Errorf("GitHub did not return an access token")
	}
	s.mu.Lock()
	user := s.user
	s.mu.Unlock()
	if user.Email == "" {
		var err error
		user, err = s.fetchUser(ctx, tokenResponse.AccessToken)
		if err != nil {
			return identity.GitHubSession{}, err
		}
	}
	return s.sessionFromTokenResponse(tokenResponse.AccessToken, tokenResponse.TokenType, tokenResponse.ExpiresIn, tokenResponse.RefreshToken, tokenResponse.RefreshTokenExpiresIn, user), nil
}

func (s *DeviceSession) sessionFromTokenResponse(accessToken string, tokenType string, expiresIn int, refreshToken string, refreshExpiresIn int, user identity.User) identity.GitHubSession {
	now := s.now().UTC()
	session := identity.GitHubSession{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		User:         user,
		UpdatedAt:    now,
	}
	if session.TokenType == "" {
		session.TokenType = "bearer"
	}
	if expiresIn > 0 {
		session.AccessTokenExpiresAt = now.Add(time.Duration(expiresIn) * time.Second)
	}
	if refreshExpiresIn > 0 {
		session.RefreshTokenExpiresAt = now.Add(time.Duration(refreshExpiresIn) * time.Second)
	}
	return session
}

func (s *DeviceSession) applySessionLocked(session identity.GitHubSession) {
	s.accessToken = session.AccessToken
	s.accessTokenExpiresAt = session.AccessTokenExpiresAt
	s.refreshToken = session.RefreshToken
	s.refreshTokenExpiresAt = session.RefreshTokenExpiresAt
	s.tokenType = session.TokenType
	s.user = session.User
}

func (s *DeviceSession) clearLocked() {
	s.pending = deviceFlow{}
	s.accessToken = ""
	s.accessTokenExpiresAt = time.Time{}
	s.refreshToken = ""
	s.refreshTokenExpiresAt = time.Time{}
	s.tokenType = ""
	s.user = identity.User{}
	s.lastError = ""
}

func (s *DeviceSession) accessExpiredLocked() bool {
	if s.accessTokenExpiresAt.IsZero() {
		return false
	}
	return !s.now().Before(s.accessTokenExpiresAt.Add(-tokenRefreshSkew))
}

func (s *DeviceSession) usableLocked() bool {
	if s.accessToken == "" {
		return false
	}
	if !s.accessExpiredLocked() {
		return true
	}
	return s.refreshToken != "" && !s.refreshExpiredLocked()
}

func (s *DeviceSession) refreshExpiredLocked() bool {
	if s.refreshToken == "" || s.refreshTokenExpiresAt.IsZero() {
		return false
	}
	return !s.now().Before(s.refreshTokenExpiresAt)
}

func (s *DeviceSession) savePersistedSession(ctx context.Context, session identity.GitHubSession) error {
	if s.store == nil {
		return nil
	}
	return s.store.SaveGitHubSession(ctx, session)
}

func (s *DeviceSession) deletePersistedSession(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	return s.store.DeleteGitHubSession(ctx)
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
