package compose

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

func TestGenerateShadowOverride(t *testing.T) {
	tests := []struct {
		name          string
		app           *config.AppConfig
		version       string
		hasEnvFile    bool
		internalHosts []string
		nodePrivateIP string
		compose       string
		check         func(t *testing.T, override OverrideFile, raw string)
	}{
		{
			name:    "sets traefik.enable=false on all services",
			app:     releaseApp("client", "ghcr.io/org/client", "v1.0.0"),
			version: "v2.0.0",
			compose: `services:
  client:
    image: ghcr.io/org/client
  redis:
    image: redis:7
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				for _, svcName := range []string{"client", "redis"} {
					svc, ok := o.Services[svcName]
					if !ok {
						t.Fatalf("expected service %s in override", svcName)
					}
					if svc.Labels["traefik.enable"] != "false" {
						t.Errorf("service %s: expected traefik.enable=false, got %q", svcName, svc.Labels["traefik.enable"])
					}
				}
			},
		},
		{
			name:    "sets image tag from shadow version",
			app:     releaseApp("client", "ghcr.io/org/client", "v1.0.0"),
			version: "v2.0.0",
			compose: `services:
  client:
    image: ghcr.io/org/client
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				if o.Services["client"].Image != "ghcr.io/org/client:v2.0.0" {
					t.Errorf("expected ghcr.io/org/client:v2.0.0, got %s", o.Services["client"].Image)
				}
			},
		},
		{
			name:    "does not set image on non-primary services",
			app:     releaseApp("client", "ghcr.io/org/client", "v1.0.0"),
			version: "v2.0.0",
			compose: `services:
  client:
    image: ghcr.io/org/client
  redis:
    image: redis:7
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				if o.Services["redis"].Image != "" {
					t.Errorf("expected no image override for redis, got %s", o.Services["redis"].Image)
				}
			},
		},
		{
			name: "adds shadow hostname to extra_hosts when internal expose set",
			app: &config.AppConfig{
				Name: "client",
				Release: config.ReleaseConfig{Services: map[string]config.ReleaseServiceConfig{
					"client": {
						Image:  "ghcr.io/org/client",
						Port:   8080,
						Expose: &config.ExposeConfig{Internal: &config.InternalExpose{Hostname: "client.internal"}},
					},
				}},
			},
			version:       "v2.0.0",
			internalHosts: []string{"client.internal", "auth.internal"},
			nodePrivateIP: "100.64.0.10",
			compose: `services:
  client:
    image: ghcr.io/org/client
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				hosts := o.Services["client"].ExtraHosts
				hostMap := make(map[string]bool)
				for _, h := range hosts {
					hostMap[h] = true
				}
				if !hostMap["client.internal:100.64.0.10"] {
					t.Error("expected client.internal:100.64.0.10 in extra_hosts")
				}
				if !hostMap["auth.internal:100.64.0.10"] {
					t.Error("expected auth.internal:100.64.0.10 in extra_hosts")
				}
				if !hostMap["shadow-client.internal:100.64.0.10"] {
					t.Error("expected shadow-client.internal:100.64.0.10 in extra_hosts")
				}
			},
		},
		{
			name:          "no shadow hostname when no internal expose",
			app:           releaseApp("worker", "ghcr.io/org/worker", "v1.0.0"),
			version:       "v1.1.0",
			internalHosts: []string{"worker.internal"},
			nodePrivateIP: "100.64.0.10",
			compose: `services:
  worker:
    image: ghcr.io/org/worker
`,
			check: func(t *testing.T, o OverrideFile, raw string) {
				if strings.Contains(raw, "shadow-") {
					t.Error("expected no shadow- hostname when app has no internal expose")
				}
				hosts := o.Services["worker"].ExtraHosts
				if len(hosts) != 1 {
					t.Fatalf("expected 1 extra_host, got %d: %v", len(hosts), hosts)
				}
			},
		},
		{
			name:    "defaults to latest when version is empty",
			app:     releaseApp("app", "ghcr.io/org/app", ""),
			version: "",
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
			name: "does not inject persistent volume overrides into shadow deploys",
			app: &config.AppConfig{
				Name: "probe",
				Release: config.ReleaseConfig{Services: map[string]config.ReleaseServiceConfig{
					"probe": {Image: "ghcr.io/org/probe", ImageTag: "v1.0.0"},
				}},
				PersistentVolumes: []config.PersistentVolumeConfig{
					{ComposeName: "probe-data", Name: "warpgate-probe-data"},
				},
			},
			version: "v1.1.0",
			compose: `services:
  probe:
    image: ghcr.io/org/probe
    volumes:
      - probe-data:/data

volumes:
  probe-data:
`,
			check: func(t *testing.T, o OverrideFile, raw string) {
				if len(o.Volumes) != 0 {
					t.Errorf("expected no top-level volume overrides in shadow deploy, got %+v", o.Volumes)
				}
				if strings.Contains(raw, "warpgate-probe-data") {
					t.Error("shadow override must not pin live persistent volume names")
				}
			},
		},
		{
			name: "release services get their own env_file when env file is available",
			app: &config.AppConfig{
				Name: "client",
				Release: config.ReleaseConfig{
					Services: map[string]config.ReleaseServiceConfig{
						"client": {
							Image: "ghcr.io/org/client",
							Environment: map[string]string{
								"LOG_LEVEL": "debug",
							},
						},
						"admin": {
							Image:         "ghcr.io/org/admin",
							SecretsPrefix: "client/admin",
						},
					},
				},
			},
			version:    "v2.0.0",
			hasEnvFile: true,
			compose: `services:
  client:
    image: ghcr.io/org/client
  admin:
    image: ghcr.io/org/admin
  redis:
    image: redis:7
`,
			check: func(t *testing.T, o OverrideFile, _ string) {
				if len(o.Services["client"].EnvFile) != 1 || o.Services["client"].EnvFile[0] != ".env.client" {
					t.Errorf("expected client env_file .env.client, got %v", o.Services["client"].EnvFile)
				}
				if len(o.Services["admin"].EnvFile) != 1 || o.Services["admin"].EnvFile[0] != ".env.admin" {
					t.Errorf("expected admin env_file .env.admin, got %v", o.Services["admin"].EnvFile)
				}
				if len(o.Services["redis"].EnvFile) != 0 {
					t.Errorf("redis should not get env_file, got %v", o.Services["redis"].EnvFile)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envFiles := map[string]bool{}
			if tt.hasEnvFile {
				envFiles = map[string]bool{"client": true, "admin": true}
			}
			raw, err := GenerateShadowOverrideWithEnvFiles(tt.app, tt.version, envFiles, tt.internalHosts, tt.nodePrivateIP, tt.compose)
			if err != nil {
				t.Fatalf("GenerateShadowOverride() error: %v", err)
			}

			var override OverrideFile
			if err := yaml.Unmarshal([]byte(raw), &override); err != nil {
				t.Fatalf("failed to parse override YAML: %v", err)
			}

			tt.check(t, override, raw)
		})
	}
}

func TestGenerateShadowOverrideInvalidCompose(t *testing.T) {
	app := releaseApp("api", "ghcr.io/org/api", "v1.0.0")

	_, err := GenerateShadowOverride(app, "v2.0.0", nil, "100.64.0.10", "not: valid: yaml: [")
	if err == nil {
		t.Error("expected error for invalid compose YAML")
	}
}

func TestGenerateShadowOverrideFallsBackToAppName(t *testing.T) {
	app := releaseApp("api", "ghcr.io/org/api", "v1.0.0")

	raw, err := GenerateShadowOverride(app, "v2.0.0", nil, "100.64.0.10", `services: {}`)
	if err != nil {
		t.Fatalf("GenerateShadowOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(raw), &override); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	if _, ok := override.Services["api"]; !ok {
		t.Error("expected fallback to app name when compose has no services")
	}
}
