package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	// EnvAppID names the env var holding the GitHub App ID.
	EnvAppID = "WARPGATE_GH_APP_ID"
	// EnvInstallationID names the env var holding the GitHub App installation ID.
	EnvInstallationID = "WARPGATE_GH_INSTALLATION_ID"
	// EnvPrivateKeyFile names the env var holding a path to the GitHub App private key PEM file.
	// The key is only ever read from a file; PEM content in the environment is not supported
	// because process environments leak through inspection tools, unit files, and logs.
	EnvPrivateKeyFile = "WARPGATE_GH_PRIVATE_KEY_FILE"

	appJWTLifetime        = 9 * time.Minute
	appJWTClockSkew       = time.Minute
	installationTokenSkew = 2 * time.Minute
)

// AppConfig holds GitHub App credentials for installation token minting.
type AppConfig struct {
	// AppID is the GitHub App identifier.
	AppID int64
	// InstallationID is the installation to mint tokens for.
	InstallationID int64
	// PrivateKeyPEM is the app's RSA private key in PEM form.
	PrivateKeyPEM []byte
}

// AppConfigFromEnv loads GitHub App credentials from the environment.
func AppConfigFromEnv() (AppConfig, error) {
	appID, err := parseEnvInt(EnvAppID)
	if err != nil {
		return AppConfig{}, err
	}
	installationID, err := parseEnvInt(EnvInstallationID)
	if err != nil {
		return AppConfig{}, err
	}
	keyPEM, err := privateKeyFromEnv()
	if err != nil {
		return AppConfig{}, err
	}
	return AppConfig{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKeyPEM:  keyPEM,
	}, nil
}

func parseEnvInt(name string) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("%s is not set", name)
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return value, nil
}

func privateKeyFromEnv() ([]byte, error) {
	path := os.Getenv(EnvPrivateKeyFile)
	if path == "" {
		return nil, fmt.Errorf("%s is not set", EnvPrivateKeyFile)
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", EnvPrivateKeyFile, err)
	}
	return key, nil
}

// AppTokenProvider mints GitHub App installation tokens on demand.
type AppTokenProvider struct {
	mu sync.Mutex

	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	apiURL         string
	httpClient     *http.Client
	now            func() time.Time

	token          string
	tokenExpiresAt time.Time
}

// NewAppTokenProvider validates GitHub App credentials and creates a token provider.
func NewAppTokenProvider(cfg AppConfig) (*AppTokenProvider, error) {
	if cfg.AppID <= 0 {
		return nil, errors.New("github app id is required")
	}
	if cfg.InstallationID <= 0 {
		return nil, errors.New("github app installation id is required")
	}
	key, err := parseRSAPrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	return &AppTokenProvider{
		appID:          cfg.AppID,
		installationID: cfg.InstallationID,
		privateKey:     key,
		apiURL:         defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		now: time.Now,
	}, nil
}

// Token returns a valid installation token, minting a new one when needed.
func (p *AppTokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	freshUntil := p.now().Add(installationTokenSkew)
	if p.token != "" && freshUntil.Before(p.tokenExpiresAt) {
		return p.token, nil
	}
	token, expiresAt, err := p.mintInstallationToken(ctx)
	if err != nil {
		return "", err
	}
	p.token = token
	p.tokenExpiresAt = expiresAt
	return p.token, nil
}

func (p *AppTokenProvider) mintInstallationToken(ctx context.Context) (string, time.Time, error) {
	appJWT, err := p.signAppJWT()
	if err != nil {
		return "", time.Time{}, err
	}
	endpoint := p.apiURL + "/app/installations/" + strconv.FormatInt(p.installationID, 10) + "/access_tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+appJWT)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, installationTokenError(resp)
	}
	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return "", time.Time{}, fmt.Errorf("decode installation token response: %w", err)
	}
	if payload.Token == "" {
		return "", time.Time{}, errors.New("github installation token response missing token")
	}
	return payload.Token, payload.ExpiresAt, nil
}

func installationTokenError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return fmt.Errorf("github installation token request failed: %s", resp.Status)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		return fmt.Errorf("github installation token request failed: %s: %s", resp.Status, payload.Message)
	}
	return fmt.Errorf("github installation token request failed: %s", resp.Status)
}

func (p *AppTokenProvider) signAppJWT() (string, error) {
	now := p.now()
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	claims := map[string]any{
		"iat": now.Add(-appJWTClockSkew).Unix(),
		"exp": now.Add(appJWTLifetime).Unix(),
		"iss": strconv.FormatInt(p.appID, 10),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, p.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseRSAPrivateKey(keyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return key, nil
}
