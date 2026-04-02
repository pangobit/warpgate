// Package secrets provides a client for fetching secrets from a SecretSauce server.
package secrets

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client fetches decrypted secrets from a SecretSauce server.
type Client struct {
	// BaseURL is the SecretSauce server URL.
	BaseURL string
	// HTTPClient is the HTTP client used for requests.
	HTTPClient *http.Client
}

// NewClient creates a new secrets client for the given server URL.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type listEntry struct {
	Key   string `json:"key"`
	State string `json:"state"`
}

type getResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	State string `json:"state"`
}

// FetchEnv returns all active secrets under prefix as a map of environment
// variable names to values. Keys are converted using the same logic as
// SecretSauce's runtime: strip prefix, remove leading slash, replace
// slashes with underscores, uppercase.
func (c *Client) FetchEnv(prefix string) (map[string]string, error) {
	keys, err := c.listSecrets(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	env := make(map[string]string, len(keys))
	for _, key := range keys {
		val, err := c.getSecret(key)
		if err != nil {
			return nil, fmt.Errorf("failed to get secret %s: %w", key, err)
		}
		name := KeyToEnvVar(key, prefix)
		env[name] = val
	}

	return env, nil
}

func (c *Client) listSecrets(prefix string) ([]string, error) {
	u := c.BaseURL + "/api/secrets"
	if prefix != "" {
		u += "?prefix=" + url.QueryEscape(prefix)
	}

	resp, err := c.HTTPClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("vault is sealed")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var entries []listEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var keys []string
	for _, e := range entries {
		if e.State == "active" {
			keys = append(keys, e.Key)
		}
	}
	return keys, nil
}

func (c *Client) getSecret(key string) (string, error) {
	u := c.BaseURL + "/api/secrets/" + url.PathEscape(key)

	resp, err := c.HTTPClient.Get(u)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return "", fmt.Errorf("vault is sealed")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d for key %s", resp.StatusCode, key)
	}

	var r getResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return r.Value, nil
}

// KeyToEnvVar converts a secret key to an environment variable name by
// stripping the prefix, removing the leading slash, replacing remaining
// slashes with underscores, and uppercasing.
func KeyToEnvVar(key, prefix string) string {
	s := key
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		s = key[len(prefix):]
	}
	s = strings.TrimPrefix(s, "/")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ToUpper(s)
	return s
}

// FormatDotEnv renders a key=value map as a .env file string.
// Values containing special characters are quoted.
func FormatDotEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		v := env[k]
		if needsQuoting(v) {
			v = "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String()
}

func needsQuoting(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '"' || c == '\'' || c == '\\' || c == '$' ||
			c == '`' || c == '!' || c == '#' || c == '\n' || c == '\t' {
			return true
		}
	}
	return false
}
