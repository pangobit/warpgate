// Package registry implements container registry metadata connectors.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	manifestAccept  = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json"
	tagsPageSize    = "100"
	ghcrRegistryURL = "https://ghcr.io"
)

// TokenProvider provides GitHub tokens used to authenticate against GHCR.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// StaticToken is a TokenProvider for a fixed registry credential, such as a
// classic PAT with read:packages. GHCR does not accept GitHub App installation
// tokens, so private image reads require one of these.
type StaticToken string

// Token returns the fixed registry credential.
func (t StaticToken) Token(_ context.Context) (string, error) {
	return string(t), nil
}

// GHCR reads GHCR image metadata: tag lists and tag digests.
type GHCR struct {
	httpClient    *http.Client
	registryURL   string
	tokenProvider TokenProvider
}

// NewGHCR creates a GHCR registry connector for anonymous access.
func NewGHCR() *GHCR {
	return &GHCR{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		registryURL: ghcrRegistryURL,
	}
}

// NewGHCRWithTokenProvider creates a GHCR registry connector that authenticates with GitHub tokens.
func NewGHCRWithTokenProvider(provider TokenProvider) *GHCR {
	connector := NewGHCR()
	connector.tokenProvider = provider
	return connector
}

// ResolveDigest resolves an image tag to a registry digest.
func (g *GHCR) ResolveDigest(ctx context.Context, image string, tag string) (string, error) {
	repo, err := ghcrRepository(image)
	if err != nil {
		return "", err
	}
	registryToken, err := g.registryToken(ctx, repo)
	if err != nil {
		return "", err
	}
	endpoint := g.registryURL + "/v2/" + repo + "/manifests/" + tag
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", manifestAccept)
	if registryToken != "" {
		req.Header.Set("Authorization", "Bearer "+registryToken)
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("resolve %s:%s: %s", image, tag, resp.Status)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("resolve %s:%s: missing Docker-Content-Digest", image, tag)
	}
	return digest, nil
}

// ListTags lists all tags for an image, following registry pagination.
func (g *GHCR) ListTags(ctx context.Context, image string) ([]string, error) {
	repo, err := ghcrRepository(image)
	if err != nil {
		return nil, err
	}
	registryToken, err := g.registryToken(ctx, repo)
	if err != nil {
		return nil, err
	}
	var tags []string
	endpoint := g.registryURL + "/v2/" + repo + "/tags/list?n=" + tagsPageSize
	for endpoint != "" {
		page, next, err := g.tagsPage(ctx, image, endpoint, registryToken)
		if err != nil {
			return nil, err
		}
		tags = append(tags, page...)
		endpoint = next
	}
	return tags, nil
}

func (g *GHCR) tagsPage(ctx context.Context, image string, endpoint string, registryToken string) ([]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	if registryToken != "" {
		req.Header.Set("Authorization", "Bearer "+registryToken)
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("list tags for %s: %s", image, resp.Status)
	}
	var payload struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, "", fmt.Errorf("decode tags for %s: %w", image, err)
	}
	next, err := nextPageURL(g.registryURL, resp.Header.Get("Link"))
	if err != nil {
		return nil, "", fmt.Errorf("list tags for %s: %w", image, err)
	}
	return payload.Tags, next, nil
}

// registryToken exchanges GitHub credentials (or anonymous access) for a GHCR pull token.
func (g *GHCR) registryToken(ctx context.Context, repo string) (string, error) {
	endpoint := g.registryURL + "/token?service=ghcr.io&scope=" + url.QueryEscape("repository:"+repo+":pull")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if g.tokenProvider != nil {
		githubToken, err := g.tokenProvider.Token(ctx)
		if err != nil {
			return "", fmt.Errorf("ghcr token for %s: %w", repo, err)
		}
		req.SetBasicAuth("x-access-token", githubToken)
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ghcr token for %s: %s", repo, resp.Status)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode ghcr token for %s: %w", repo, err)
	}
	if payload.Token == "" {
		return "", fmt.Errorf("ghcr token response for %s missing token", repo)
	}
	return payload.Token, nil
}

func ghcrRepository(image string) (string, error) {
	if !strings.HasPrefix(image, "ghcr.io/") {
		return "", fmt.Errorf("unsupported registry for %s", image)
	}
	return strings.TrimPrefix(image, "ghcr.io/"), nil
}

// nextPageURL resolves the registry pagination Link header against the registry base URL.
func nextPageURL(base string, link string) (string, error) {
	if link == "" {
		return "", nil
	}
	start := strings.Index(link, "<")
	end := strings.Index(link, ">")
	if start == -1 || end == -1 || end < start {
		return "", fmt.Errorf("malformed Link header %q", link)
	}
	ref, err := url.Parse(link[start+1 : end])
	if err != nil {
		return "", err
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(ref).String(), nil
}
