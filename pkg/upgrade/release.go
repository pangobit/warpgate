package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	defaultRepoOwner = "pangobit"
	defaultRepoName  = "warpgate"
	checksumsAsset   = "checksums.txt"
)

// Release describes a GitHub release and its downloadable assets.
type Release struct {
	// Tag is the release tag name.
	Tag string
	// AssetName is the platform-specific binary asset name.
	AssetName string
	// AssetURL is the download URL for the binary asset.
	AssetURL string
	// ChecksumsURL is the download URL for checksums.txt.
	ChecksumsURL string
}

// GitHubReleaseClient fetches release metadata from the GitHub API.
type GitHubReleaseClient struct {
	// Owner is the GitHub repository owner.
	Owner string
	// Name is the GitHub repository name.
	Name string
	// APIBase is the GitHub API root URL.
	APIBase string
	// HTTPClient performs GitHub API requests.
	HTTPClient *http.Client
}

// FetchRelease resolves a release tag and asset URLs for the given architecture.
func (c *GitHubReleaseClient) FetchRelease(ctx context.Context, version, goos, goarch string) (*Release, error) {
	owner := c.Owner
	if owner == "" {
		owner = defaultRepoOwner
	}
	name := c.Name
	if name == "" {
		name = defaultRepoName
	}

	assetName, err := assetNameForPlatform(goos, goarch)
	if err != nil {
		return nil, err
	}

	apiBase := strings.TrimRight(c.APIBase, "/")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}

	var endpoint string
	switch strings.ToLower(strings.TrimSpace(version)) {
	case "", "latest":
		endpoint = apiBase + "/repos/" + owner + "/" + name + "/releases/latest"
	default:
		tag := normalizeTag(version)
		endpoint = apiBase + "/repos/" + owner + "/" + name + "/releases/tags/" + tag
	}

	payload, err := c.getJSON(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var response struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("parse release response: %w", err)
	}
	if response.TagName == "" {
		return nil, fmt.Errorf("release response missing tag name")
	}

	release := &Release{
		Tag:       normalizeTag(response.TagName),
		AssetName: assetName,
	}
	for _, asset := range response.Assets {
		switch asset.Name {
		case assetName:
			release.AssetURL = asset.BrowserDownloadURL
		case checksumsAsset:
			release.ChecksumsURL = asset.BrowserDownloadURL
		}
	}
	if release.AssetURL == "" {
		return nil, fmt.Errorf("release %s has no asset %q", release.Tag, assetName)
	}
	if release.ChecksumsURL == "" {
		return nil, fmt.Errorf("release %s has no asset %q", release.Tag, checksumsAsset)
	}
	return release, nil
}

func (c *GitHubReleaseClient) getJSON(ctx context.Context, endpoint string) ([]byte, error) {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release metadata: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read release metadata: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch release metadata: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// assetNameForPlatform returns the release asset name for a platform.
func assetNameForPlatform(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64", "linux/arm64":
		return "warpgate-" + goos + "-" + goarch, nil
	default:
		return "", fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
}

// parseChecksums maps asset names to expected SHA-256 digests.
func parseChecksums(data []byte) (map[string]string, error) {
	checksums := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid checksum line: %q", line)
		}
		checksums[parts[1]] = parts[0]
	}
	if len(checksums) == 0 {
		return nil, fmt.Errorf("checksums file is empty")
	}
	return checksums, nil
}

func githubToken() string {
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func normalizeTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return tag
	}
	if !strings.HasPrefix(tag, "v") {
		return "v" + tag
	}
	return tag
}
