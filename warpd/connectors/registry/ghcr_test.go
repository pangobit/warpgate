package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pangobit/warpgate/warpd/usecase"
)

type staticTokenProvider struct {
	token string
}

// Token returns the static fake GitHub token.
func (p staticTokenProvider) Token(_ context.Context) (string, error) {
	return p.token, nil
}

func TestListTagsPaginates(t *testing.T) {
	var sawBasicAuth bool
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		sawBasicAuth = ok && password == "ghs_token"
		if err := json.NewEncoder(w).Encode(map[string]string{"token": "registry_token"}); err != nil {
			t.Errorf("encode token: %v", err)
		}
	})
	mux.HandleFunc("/v2/pangobit/myapp/tags/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer registry_token" {
			t.Errorf("missing registry bearer token, got %q", r.Header.Get("Authorization"))
		}
		page := map[string]any{"name": "pangobit/myapp"}
		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/pangobit/myapp/tags/list?n=100&last=1.2.0>; rel="next"`)
			page["tags"] = []string{"1.0.0", "1.2.0"}
		} else {
			page["tags"] = []string{"1.2.1", "latest"}
		}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Errorf("encode tags: %v", err)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	connector := NewGHCRWithTokenProvider(staticTokenProvider{token: "ghs_token"})
	connector.registryURL = server.URL

	tags, err := connector.ListTags(t.Context(), "ghcr.io/pangobit/myapp")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	want := []string{"1.0.0", "1.2.0", "1.2.1", "latest"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for index := range want {
		if tags[index] != want[index] {
			t.Fatalf("tags = %v, want %v", tags, want)
		}
	}
	if !sawBasicAuth {
		t.Fatal("token endpoint should receive the GitHub token via basic auth")
	}
}

func TestListTagsRejectsForeignRegistry(t *testing.T) {
	connector := NewGHCR()
	_, err := connector.ListTags(t.Context(), "docker.io/library/nginx")
	if err == nil {
		t.Fatal("expected error for non-GHCR image")
	}
	if !errors.Is(err, usecase.ErrUnsupportedRegistry) {
		t.Fatalf("error = %v, want ErrUnsupportedRegistry", err)
	}
}

func TestResolveDigestUsesRegistryToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]string{"token": "registry_token"}); err != nil {
			t.Errorf("encode token: %v", err)
		}
	})
	mux.HandleFunc("/v2/pangobit/myapp/manifests/1.2.3", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer registry_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:abc")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	connector := NewGHCR()
	connector.registryURL = server.URL

	digest, err := connector.ResolveDigest(t.Context(), "ghcr.io/pangobit/myapp", "1.2.3")
	if err != nil {
		t.Fatalf("ResolveDigest: %v", err)
	}
	if digest != "sha256:abc" {
		t.Fatalf("digest = %q, want sha256:abc", digest)
	}
}
