package compose

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

func TestGenerateOverride(t *testing.T) {
	tests := []struct {
		name          string
		app           *config.AppConfig
		internalHosts []string
		compose       string
		check         func(t *testing.T, override OverrideFile, raw string)
	}{
		{
			name: "image tag from version",
			app: &config.AppConfig{
				Name:    "api",
				Image:   "ghcr.io/org/api",
				Version: "v2.0.0",
			},
			compose: `services:
  api:
    image: ghcr.io/org/api
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				if o.Services["api"].Image != "ghcr.io/org/api:v2.0.0" {
					t.Errorf("expected ghcr.io/org/api:v2.0.0, got %s", o.Services["api"].Image)
				}
			},
		},
		{
			name: "defaults to latest when version is empty",
			app: &config.AppConfig{
				Name:  "app",
				Image: "ghcr.io/org/app",
			},
			compose: `services:
  app:
    image: ghcr.io/org/app
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				if o.Services["app"].Image != "ghcr.io/org/app:latest" {
					t.Errorf("expected ghcr.io/org/app:latest, got %s", o.Services["app"].Image)
				}
			},
		},
		{
			name: "extra_hosts from internal hosts",
			app: &config.AppConfig{
				Name:    "auth",
				Image:   "ghcr.io/org/auth",
				Version: "v1.0.0",
			},
			internalHosts: []string{"auth.internal", "api.internal"},
			compose: `services:
  auth:
    image: ghcr.io/org/auth
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				svc := o.Services["auth"]
				if len(svc.ExtraHosts) != 2 {
					t.Fatalf("expected 2 extra_hosts, got %d", len(svc.ExtraHosts))
				}
				if svc.ExtraHosts[0] != "auth.internal:host-gateway" {
					t.Errorf("expected auth.internal:host-gateway, got %q", svc.ExtraHosts[0])
				}
				if svc.ExtraHosts[1] != "api.internal:host-gateway" {
					t.Errorf("expected api.internal:host-gateway, got %q", svc.ExtraHosts[1])
				}
			},
		},
		{
			name: "no extra_hosts when no internal hosts",
			app: &config.AppConfig{
				Name:    "worker",
				Image:   "ghcr.io/org/worker",
				Version: "v1.0.0",
			},
			compose: `services:
  worker:
    image: ghcr.io/org/worker
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				if len(o.Services["worker"].ExtraHosts) != 0 {
					t.Errorf("expected no extra_hosts, got %v", o.Services["worker"].ExtraHosts)
				}
			},
		},
		{
			name: "multiple services get extra_hosts but only main gets image",
			app: &config.AppConfig{
				Name:    "auth",
				Image:   "ghcr.io/org/auth",
				Version: "v1.0.0",
			},
			internalHosts: []string{"auth.internal"},
			compose: `services:
  auth:
    image: ghcr.io/org/auth
  litestream:
    image: litestream/litestream
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				if o.Services["auth"].Image != "ghcr.io/org/auth:v1.0.0" {
					t.Errorf("auth should have image override, got %s", o.Services["auth"].Image)
				}
				if o.Services["litestream"].Image != "" {
					t.Errorf("litestream should not have image override, got %s", o.Services["litestream"].Image)
				}
				for _, svcName := range []string{"auth", "litestream"} {
					if len(o.Services[svcName].ExtraHosts) != 1 {
						t.Errorf("service %s: expected 1 extra_host, got %d", svcName, len(o.Services[svcName].ExtraHosts))
					}
				}
			},
		},
		{
			name: "no traefik labels or network even when app has expose config",
			app: &config.AppConfig{
				Name:    "api",
				Image:   "ghcr.io/org/api",
				Version: "v1.0.0",
				Port:    3000,
				Expose: &config.ExposeConfig{
					Public: &config.PublicExpose{Domains: []string{"api.example.com"}},
				},
			},
			compose: `services:
  api:
    image: ghcr.io/org/api
`,
			check: func(t *testing.T, _ OverrideFile, raw string) {
				if strings.Contains(raw, "traefik") {
					t.Error("override should not contain traefik labels")
				}
				if strings.Contains(raw, "warpgate") {
					t.Error("override should not contain warpgate network")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := GenerateOverride(tt.app, &config.NetworkingConfig{}, tt.internalHosts, tt.compose)
			if err != nil {
				t.Fatalf("GenerateOverride() error: %v", err)
			}

			var override OverrideFile
			if err := yaml.Unmarshal([]byte(output), &override); err != nil {
				t.Fatalf("failed to parse generated YAML: %v", err)
			}

			tt.check(t, override, output)
		})
	}
}

func TestGenerateTraefikCompose(t *testing.T) {
	networking := &config.NetworkingConfig{
		Traefik: config.TraefikConfig{
			EntryPoints: []string{"web", "websecure"},
			ACME: config.ACMEConfig{
				Enabled:  true,
				Email:    "admin@example.com",
				Provider: "letsencrypt",
				Staging:  true,
			},
		},
	}

	output, err := GenerateTraefikCompose(networking)
	if err != nil {
		t.Fatalf("GenerateTraefikCompose() error: %v", err)
	}

	if !strings.Contains(output, "traefik:v3.6") {
		t.Error("expected traefik:v3.6 image")
	}
	if !strings.Contains(output, "acme-staging") {
		t.Error("expected staging ACME server")
	}
	if !strings.Contains(output, "80:80") {
		t.Error("expected port 80 mapping")
	}
	if !strings.Contains(output, "443:443") {
		t.Error("expected port 443 mapping")
	}
	if strings.Contains(output, "8080:8080") {
		t.Error("public Traefik should not have internal entrypoint 8080")
	}
	if strings.Contains(output, "providers.file.directory") {
		t.Error("public Traefik should not have file provider (moved to internal proxy)")
	}
}
