// Package github implements the Warpgate GitHub repository connector.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/usecase"
)

const defaultBaseURL = "https://api.github.com"

// ErrNotFound indicates that GitHub returned a not found response.
var ErrNotFound = errors.New("github resource not found")

// APIError describes a failed GitHub API response.
type APIError struct {
	// StatusCode is the HTTP response status code.
	StatusCode int
	// Status is the HTTP response status.
	Status string
	// Method is the HTTP request method.
	Method string
	// Endpoint is the GitHub API endpoint.
	Endpoint string
	// Message is GitHub's response message.
	Message string
}

// Error returns the sanitized GitHub API error.
func (e APIError) Error() string {
	if e.Message == "" {
		return "github " + e.Method + " " + e.Endpoint + " failed: " + e.Status
	}
	return "github " + e.Method + " " + e.Endpoint + " failed: " + e.Status + ": " + e.Message
}

// Is reports whether the API error matches a sentinel.
func (e APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

// TokenProvider provides GitHub access tokens for API requests.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// Client calls the GitHub Contents and Git Data APIs.
type Client struct {
	baseURL       string
	httpClient    *http.Client
	tokenProvider TokenProvider
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

// NewClientWithTokenProvider creates a GitHub client using local GitHub tokens.
func NewClientWithTokenProvider(provider TokenProvider) *Client {
	client := NewClient()
	client.tokenProvider = provider
	return client
}

// BranchHead returns the commit SHA at a branch head.
func (c *Client) BranchHead(ctx context.Context, settings configrepo.RepositorySettings) (string, error) {
	var response struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.getJSON(ctx, settings, "/repos/"+settings.Owner+"/"+settings.Repo+"/branches/"+settings.Branch, &response); err != nil {
		if errors.Is(err, ErrNotFound) {
			if message := c.repositoryAccessMessage(ctx, settings); message != "" {
				return "", errors.New(message)
			}
			return "", fmt.Errorf("GitHub repository or branch not found: %s/%s@%s. If this is a private repository, authorize the GitHub App and install it for this repository.", settings.Owner, settings.Repo, settings.Branch)
		}
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
	root := repositoryPathPrefix(settings)
	for _, item := range tree.Tree {
		if item.Type != "blob" || !isAppConfigPath(root, item.Path) {
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
	if err := c.authorize(ctx, req, settings); err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return githubAPIError(resp, method, endpoint)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) getAuthorizedJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if err := c.authorize(ctx, req, configrepo.RepositorySettings{}); err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return githubAPIError(resp, http.MethodGet, endpoint)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) authorize(ctx context.Context, req *http.Request, settings configrepo.RepositorySettings) error {
	if c.tokenProvider != nil {
		token, err := c.tokenProvider.Token(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	} else if settings.TokenEnvVar != "" {
		token := os.Getenv(settings.TokenEnvVar)
		if token == "" {
			return fmt.Errorf("%s is not set", settings.TokenEnvVar)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

func (c *Client) repositoryAccessMessage(ctx context.Context, settings configrepo.RepositorySettings) string {
	if c.tokenProvider == nil {
		return ""
	}
	diagnosis, err := c.diagnoseGitHubAppRepositoryAccess(ctx, settings)
	if err != nil {
		return ""
	}
	return diagnosis
}

func (c *Client) diagnoseGitHubAppRepositoryAccess(ctx context.Context, settings configrepo.RepositorySettings) (string, error) {
	var response struct {
		Installations []struct {
			ID                  int64             `json:"id"`
			RepositorySelection string            `json:"repository_selection"`
			Permissions         map[string]string `json:"permissions"`
			Account             struct {
				Login string `json:"login"`
			} `json:"account"`
		} `json:"installations"`
	}
	if err := c.getAuthorizedJSON(ctx, "/user/installations?per_page=100", &response); err != nil {
		return "", err
	}
	if len(response.Installations) == 0 {
		return "GitHub App is authorized but is not installed for any repositories. Install the app for " + settings.Owner + "/" + settings.Repo + ", then try again.", nil
	}
	ownerInstalled := false
	contentsAllowed := false
	for _, installation := range response.Installations {
		if !strings.EqualFold(installation.Account.Login, settings.Owner) {
			continue
		}
		ownerInstalled = true
		permission := installation.Permissions["contents"]
		if permission == "read" || permission == "write" {
			contentsAllowed = true
		}
		repoSelected, err := c.installationIncludesRepository(ctx, installation.ID, settings)
		if err != nil {
			return "", err
		}
		if repoSelected && contentsAllowed {
			return "", nil
		}
		if repoSelected {
			return "GitHub App is installed for " + settings.Owner + "/" + settings.Repo + ", but the app needs Contents: read permission.", nil
		}
	}
	if !ownerInstalled {
		return "GitHub App is authorized but is not installed for " + settings.Owner + ". Install it for " + settings.Owner + "/" + settings.Repo + ", then try again.", nil
	}
	if !contentsAllowed {
		return "GitHub App is installed for " + settings.Owner + ", but the app needs Contents: read permission.", nil
	}
	return "GitHub App is installed for " + settings.Owner + ", but " + settings.Owner + "/" + settings.Repo + " is not selected for this installation.", nil
}

func (c *Client) installationIncludesRepository(ctx context.Context, installationID int64, settings configrepo.RepositorySettings) (bool, error) {
	target := settings.Owner + "/" + settings.Repo
	for page := 1; ; page++ {
		var response struct {
			Repositories []struct {
				FullName string `json:"full_name"`
			} `json:"repositories"`
		}
		endpoint := fmt.Sprintf("/user/installations/%d/repositories?per_page=100&page=%d", installationID, page)
		if err := c.getAuthorizedJSON(ctx, endpoint, &response); err != nil {
			return false, err
		}
		for _, repository := range response.Repositories {
			if strings.EqualFold(repository.FullName, target) {
				return true, nil
			}
		}
		if len(response.Repositories) < 100 {
			return false, nil
		}
	}
}

func githubAPIError(resp *http.Response, method string, endpoint string) error {
	apiErr := APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Method:     method,
		Endpoint:   endpoint,
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if readErr != nil {
		return apiErr
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		apiErr.Message = payload.Message
		return apiErr
	}
	apiErr.Message = strings.TrimSpace(string(body))
	return apiErr
}

func repositoryPathPrefix(settings configrepo.RepositorySettings) string {
	root := strings.Trim(strings.TrimSpace(settings.Path), "/")
	if root == "" || root == "." {
		return ""
	}
	return path.Clean(root) + "/"
}

func isAppConfigPath(root string, value string) bool {
	if root != "" {
		if !strings.HasPrefix(value, root) {
			return false
		}
		value = strings.TrimPrefix(value, root)
	}
	if !strings.HasPrefix(value, "apps/") || !strings.HasSuffix(value, "/app.yml") {
		return false
	}
	parts := strings.Split(value, "/")
	return len(parts) == 3
}
