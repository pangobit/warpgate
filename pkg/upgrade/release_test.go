package upgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubReleaseClientFetchRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/pangobit/warpgate/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.3",
			"assets": [
				{"name": "warpgate-linux-amd64", "browser_download_url": "https://example.com/binary"},
				{"name": "checksums.txt", "browser_download_url": "https://example.com/checksums"}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	client := &GitHubReleaseClient{
		APIBase:    server.URL,
		HTTPClient: server.Client(),
	}

	release, err := client.FetchRelease(context.Background(), "latest", "linux", "amd64")
	if err != nil {
		t.Fatalf("FetchRelease() error = %v", err)
	}
	if release.Tag != "v1.2.3" {
		t.Fatalf("release.Tag = %q, want v1.2.3", release.Tag)
	}
	if release.AssetName != "warpgate-linux-amd64" {
		t.Fatalf("release.AssetName = %q, want warpgate-linux-amd64", release.AssetName)
	}
	if release.AssetURL != "https://example.com/binary" {
		t.Fatalf("release.AssetURL = %q", release.AssetURL)
	}
}

func TestNormalizeTag(t *testing.T) {
	if got := normalizeTag("1.2.3"); got != "v1.2.3" {
		t.Fatalf("normalizeTag() = %q, want v1.2.3", got)
	}
}
