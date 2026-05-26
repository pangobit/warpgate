// Package github implements the Warpgate GitHub repository connector.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/usecase"
)

const defaultBaseURL = "https://api.github.com"

// Client calls the GitHub Contents and Git Data APIs.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a GitHub client.
func NewClient() *Client {
	return &Client{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// BranchHead returns the commit SHA at a branch head.
func (c *Client) BranchHead(ctx context.Context, settings configrepo.RepositorySettings) (string, error) {
	var response struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.getJSON(ctx, settings, "/repos/"+settings.Owner+"/"+settings.Repo+"/branches/"+settings.Branch, &response); err != nil {
		return "", err
	}
	if response.Commit.SHA == "" {
		return "", fmt.Errorf("github branch %s has no commit SHA", settings.Branch)
	}
	return response.Commit.SHA, nil
}

// ReadFile reads one file at the given ref.
func (c *Client) ReadFile(ctx context.Context, settings configrepo.RepositorySettings, path string, ref string) (usecase.GitHubFile, error) {
	var response struct {
		Path     string `json:"path"`
		SHA      string `json:"sha"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	endpoint := "/repos/" + settings.Owner + "/" + settings.Repo + "/contents/" + path + "?ref=" + ref
	if err := c.getJSON(ctx, settings, endpoint, &response); err != nil {
		return usecase.GitHubFile{}, err
	}
	if response.Encoding != "base64" {
		return usecase.GitHubFile{}, fmt.Errorf("github file %s has unsupported encoding %q", path, response.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	if err != nil {
		return usecase.GitHubFile{}, fmt.Errorf("decode github file %s: %w", path, err)
	}
	return usecase.GitHubFile{
		Path:      response.Path,
		Content:   string(decoded),
		SHA:       response.SHA,
		CommitSHA: ref,
	}, nil
}

// ListAppConfigFiles lists all apps/*/app.yml files at the given ref.
func (c *Client) ListAppConfigFiles(ctx context.Context, settings configrepo.RepositorySettings, ref string) ([]usecase.GitHubFile, error) {
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	endpoint := "/repos/" + settings.Owner + "/" + settings.Repo + "/git/trees/" + ref + "?recursive=1"
	if err := c.getJSON(ctx, settings, endpoint, &tree); err != nil {
		return nil, err
	}
	var files []usecase.GitHubFile
	for _, item := range tree.Tree {
		if item.Type != "blob" || !isAppConfigPath(item.Path) {
			continue
		}
		file, err := c.ReadFile(ctx, settings, item.Path, ref)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// WriteFile writes one file using optimistic concurrency.
func (c *Client) WriteFile(ctx context.Context, input usecase.WriteFileInput) (usecase.GitHubFile, error) {
	payload := struct {
		Message string `json:"message"`
		Content string `json:"content"`
		SHA     string `json:"sha"`
		Branch  string `json:"branch"`
	}{
		Message: input.Message,
		Content: base64.StdEncoding.EncodeToString([]byte(input.Content)),
		SHA:     input.ExpectedSHA,
		Branch:  input.Settings.Branch,
	}
	var response struct {
		Content struct {
			Path string `json:"path"`
			SHA  string `json:"sha"`
		} `json:"content"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.doJSON(ctx, input.Settings, http.MethodPut, "/repos/"+input.Settings.Owner+"/"+input.Settings.Repo+"/contents/"+input.Path, payload, &response); err != nil {
		return usecase.GitHubFile{}, err
	}
	return usecase.GitHubFile{
		Path:      response.Content.Path,
		Content:   input.Content,
		SHA:       response.Content.SHA,
		CommitSHA: response.Commit.SHA,
	}, nil
}

func (c *Client) getJSON(ctx context.Context, settings configrepo.RepositorySettings, endpoint string, target any) error {
	return c.doJSON(ctx, settings, http.MethodGet, endpoint, nil, target)
}

func (c *Client) doJSON(ctx context.Context, settings configrepo.RepositorySettings, method string, endpoint string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if settings.TokenEnvVar != "" {
		token := os.Getenv(settings.TokenEnvVar)
		if token == "" {
			return fmt.Errorf("%s is not set", settings.TokenEnvVar)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if readErr != nil {
			return fmt.Errorf("github %s %s failed: %s", method, endpoint, resp.Status)
		}
		return fmt.Errorf("github %s %s failed: %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func isAppConfigPath(value string) bool {
	if !strings.HasPrefix(value, "apps/") || !strings.HasSuffix(value, "/app.yml") {
		return false
	}
	parts := strings.Split(value, "/")
	return len(parts) == 3
}
