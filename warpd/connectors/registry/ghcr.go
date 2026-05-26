// Package registry implements container registry metadata connectors.
package registry

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json"

// GHCR resolves GHCR tags to immutable digests.
type GHCR struct {
	httpClient *http.Client
}

// NewGHCR creates a GHCR registry connector.
func NewGHCR() *GHCR {
	return &GHCR{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

// ResolveDigest resolves an image tag to a registry digest.
func (g *GHCR) ResolveDigest(ctx context.Context, image string, tag string) (string, error) {
	if !strings.HasPrefix(image, "ghcr.io/") {
		return "", fmt.Errorf("unsupported registry for %s", image)
	}
	repo := strings.TrimPrefix(image, "ghcr.io/")
	url := "https://ghcr.io/v2/" + repo + "/manifests/" + tag
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", manifestAccept)
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
