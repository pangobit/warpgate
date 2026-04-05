// Package deploy handles fetching compose files from remote sources.
package deploy

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/pangobit/warpgate/pkg/config"
)

const githubRawTimeout = 30 * time.Second

// FetchComposeFromSource retrieves the compose file from a remote GitHub repository.
// It constructs a raw GitHub URL from the SourceConfig and returns the content.
func FetchComposeFromSource(source *config.SourceConfig) ([]byte, error) {
	url, err := BuildGitHubRawURL(source)
	if err != nil {
		return nil, err
	}
	return fetchWithRetry(url, 3)
}

// BuildGitHubRawURL constructs the raw GitHub URL for fetching compose content.
func BuildGitHubRawURL(source *config.SourceConfig) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source is nil")
	}

	composePath := source.ComposePath
	if composePath == "" {
		composePath = "compose.yml"
	}

	repo := strings.TrimPrefix(source.Repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")

	parts := strings.Split(repo, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid repo format: %s (expected github.com/owner/repo or owner/repo)", source.Repo)
	}

	owner := parts[len(parts)-2]
	repoName := parts[len(parts)-1]

	u := &url.URL{
		Scheme: "https",
		Host:   "raw.githubusercontent.com",
		Path:   path.Join(owner, repoName, source.Ref, composePath),
	}

	return u.String(), nil
}

func fetchWithRetry(url string, maxRetries int) ([]byte, error) {
	client := &http.Client{Timeout: githubRawTimeout}

	var lastErr error
	for i := range maxRetries {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("failed to fetch from %s: %w", url, lastErr)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			content, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}
			return content, nil
		}

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("compose file not found at %s (404)", url)
		}

		// Retry on 5xx errors and rate limiting
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
				continue
			}
		}

		return nil, fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, url)
	}

	return nil, lastErr
}
