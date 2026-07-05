package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestBuildReleaseManifestUsesDigestComposeAndEnvHash(t *testing.T) {
	app := &config.AppConfig{
		Name: "api",
		Release: config.ReleaseConfig{
			Services: map[string]config.ReleaseServiceConfig{
				"api": {
					Image:       "ghcr.io/acme/api",
					ImageTag:    "v1.2.3",
					ImageDigest: "sha256:abc123",
					Environment: map[string]string{
						"LOG_LEVEL": "debug",
						"APP_ENV":   "prod",
					},
					SecretsPrefix: "api/prod",
				},
			},
		},
	}
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	first := Build(app, []byte("services:\n  api:\n"), now)
	second := Build(app, []byte("services:\n  api:\n"), now)

	if first.ID == "" {
		t.Fatal("expected release ID")
	}
	if first.ID != second.ID {
		t.Fatalf("release ID is not deterministic: %q != %q", first.ID, second.ID)
	}
	if first.ImageRef != "ghcr.io/acme/api@sha256:abc123" {
		t.Errorf("ImageRef = %q, want digest ref", first.ImageRef)
	}
	if !strings.HasPrefix(first.ComposeRev, "sha256:") {
		t.Errorf("ComposeRev = %q, want sha256 digest", first.ComposeRev)
	}
	if !strings.HasPrefix(first.EnvHash, "sha256:") {
		t.Errorf("EnvHash = %q, want sha256 digest", first.EnvHash)
	}
}

func TestBuildReleaseManifestChangesIDWhenEnvChanges(t *testing.T) {
	base := &config.AppConfig{
		Name: "api",
		Release: config.ReleaseConfig{
			Services: map[string]config.ReleaseServiceConfig{
				"api": {
					Image:       "ghcr.io/acme/api",
					ImageDigest: "sha256:abc123",
					Environment: map[string]string{
						"LOG_LEVEL": "info",
					},
				},
			},
		},
	}
	changed := &config.AppConfig{
		Name: "api",
		Release: config.ReleaseConfig{
			Services: map[string]config.ReleaseServiceConfig{
				"api": {
					Image:       "ghcr.io/acme/api",
					ImageDigest: "sha256:abc123",
					Environment: map[string]string{
						"LOG_LEVEL": "debug",
					},
				},
			},
		},
	}

	first := Build(base, []byte("services:\n  api:\n"), time.Time{})
	second := Build(changed, []byte("services:\n  api:\n"), time.Time{})

	if first.ID == second.ID {
		t.Fatal("expected env change to produce a different release ID")
	}
}

func TestBuildReleaseManifestUsesDifferentComposeRefs(t *testing.T) {
	composeRefs := []string{
		"main",
		"v1.2.3",
		"f1ef18bd8dc67dd582ed1c947c11c772a66f8419",
		"sha256:0a1b2c3d4e5f",
	}

	for _, composeRef := range composeRefs {
		t.Run(composeRef, func(t *testing.T) {
			app := releaseManifestApp("bundle", composeRef)

			first := Build(app, nil, time.Time{})
			second := Build(app, nil, time.Time{})

			if first.ID == "" {
				t.Fatal("expected release ID")
			}
			if first.ID != second.ID {
				t.Fatalf("release ID is not deterministic for compose ref %q: %q != %q", composeRef, first.ID, second.ID)
			}
			if first.ComposeRev != composeRef {
				t.Errorf("ComposeRev = %q, want %q", first.ComposeRev, composeRef)
			}
		})
	}
}

func TestBuildReleaseManifestChangesIDWhenComposeRefChanges(t *testing.T) {
	first := Build(releaseManifestApp("bundle", "main"), nil, time.Time{})
	second := Build(releaseManifestApp("bundle", "f1ef18bd8dc67dd582ed1c947c11c772a66f8419"), nil, time.Time{})

	if first.ID == second.ID {
		t.Fatal("expected compose_ref change to produce a different release ID")
	}
}

func TestBuildReleaseManifestCapturesConfiguredServices(t *testing.T) {
	app := &config.AppConfig{
		Name:       "bundle",
		ComposeRef: "main",
		Release: config.ReleaseConfig{
			Services: map[string]config.ReleaseServiceConfig{
				"api": {
					Image:       "ghcr.io/acme/api",
					ImageDigest: "sha256:api",
					Environment: map[string]string{
						"LOG_LEVEL": "info",
					},
				},
				"admin": {
					Image:         "ghcr.io/acme/admin",
					ImageTag:      "v1.2.3",
					SecretsPrefix: "admin/prod",
				},
			},
		},
		Source: &config.SourceConfig{Repo: "github.com/acme/app"},
	}

	manifest := Build(app, nil, time.Time{})

	if manifest.ComposeRev != "main" {
		t.Errorf("ComposeRev = %q, want main", manifest.ComposeRev)
	}
	if len(manifest.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(manifest.Services))
	}
	if manifest.Services["api"].ImageRef != "ghcr.io/acme/api@sha256:api" {
		t.Errorf("api ImageRef = %q", manifest.Services["api"].ImageRef)
	}
	if manifest.Services["admin"].ImageRef != "ghcr.io/acme/admin:v1.2.3" {
		t.Errorf("admin ImageRef = %q", manifest.Services["admin"].ImageRef)
	}
	if manifest.Services["admin"].SecretsPrefix != "admin/prod" {
		t.Errorf("admin SecretsPrefix = %q", manifest.Services["admin"].SecretsPrefix)
	}
}

func releaseManifestApp(name, composeRef string) *config.AppConfig {
	return &config.AppConfig{
		Name:       name,
		ComposeRef: composeRef,
		Release: config.ReleaseConfig{
			Services: map[string]config.ReleaseServiceConfig{
				"api": {
					Image:       "ghcr.io/acme/api",
					ImageDigest: "sha256:api",
					Environment: map[string]string{
						"LOG_LEVEL": "info",
					},
				},
				"admin": {
					Image:         "ghcr.io/acme/admin",
					ImageTag:      "v1.2.3",
					SecretsPrefix: "admin/prod",
				},
			},
		},
		Source: &config.SourceConfig{Repo: "github.com/acme/app"},
	}
}

func TestParseManifestJSONRoundTrip(t *testing.T) {
	raw := `{
  "id": "rel-1",
  "app": "api",
  "image_ref": "ghcr.io/acme/api:v1.0.0",
  "image_tag": "v1.0.0",
  "services": {
    "api": {
      "image_ref": "ghcr.io/acme/api:v1.0.0",
      "image_tag": "v1.0.0",
      "env_hash": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    }
  }
}`

	manifest, err := ParseManifestJSON([]byte(raw))
	if err != nil {
		t.Fatalf("ParseManifestJSON() error = %v", err)
	}
	if manifest.App != "api" {
		t.Fatalf("app = %q, want api", manifest.App)
	}
	if manifest.Services["api"].ImageRef != "ghcr.io/acme/api:v1.0.0" {
		t.Fatalf("api image ref = %q", manifest.Services["api"].ImageRef)
	}
}

func TestSaveAndLoadReleaseManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := &Manifest{
		ID:         "abc123",
		App:        "api",
		ImageRef:   "ghcr.io/acme/api@sha256:abc123",
		ComposeRev: "main",
		EnvHash:    "sha256:env",
		CreatedAt:  time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
	}

	if err := Save(dir, manifest); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	for _, name := range []string{"abc123.json", "latest.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	loaded, err := Load(dir, "latest")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.ID != manifest.ID {
		t.Errorf("loaded ID = %q, want %q", loaded.ID, manifest.ID)
	}
}
