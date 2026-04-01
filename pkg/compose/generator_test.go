package compose

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

func TestGenerateMultipleApps(t *testing.T) {
	apps := []*config.AppConfig{
		{
			Name:    "auth",
			Image:   "ghcr.io/org/auth",
			Version: "v1.0.0",
			Ports:   []config.PortConfig{{Container: 8085}},
		},
		{
			Name:    "api",
			Image:   "ghcr.io/org/api",
			Version: "v2.0.0",
			Ports:   []config.PortConfig{{Container: 3000}},
		},
	}

	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	project := NewProject("test", apps, node, config.NetworkingConfig{})

	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	if len(compose.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(compose.Services))
	}

	authSvc, ok := compose.Services["auth"]
	if !ok {
		t.Fatal("auth service not found")
	}
	if authSvc.Image != "ghcr.io/org/auth:v1.0.0" {
		t.Errorf("unexpected auth image: %s", authSvc.Image)
	}

	apiSvc, ok := compose.Services["api"]
	if !ok {
		t.Fatal("api service not found")
	}
	if apiSvc.Image != "ghcr.io/org/api:v2.0.0" {
		t.Errorf("unexpected api image: %s", apiSvc.Image)
	}
}

func TestGenerateSidecarDependsOn(t *testing.T) {
	apps := []*config.AppConfig{
		{
			Name:  "auth",
			Image: "ghcr.io/org/auth",
			Sidecars: []config.SidecarConfig{
				{
					Name:    "litestream",
					Image:   "litestream/litestream:0.5.6",
					Volumes: []string{"auth-data:/data"},
					Env:     map[string]string{"URL": "s3://bucket/db"},
				},
			},
		},
	}

	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	project := NewProject("test", apps, node, config.NetworkingConfig{})

	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	sidecar, ok := compose.Services["auth-litestream"]
	if !ok {
		t.Fatal("auth-litestream service not found")
	}

	if sidecar.Restart != "unless-stopped" {
		t.Errorf("expected sidecar restart unless-stopped, got %s", sidecar.Restart)
	}

	dep, ok := sidecar.DependsOn["auth"]
	if !ok {
		t.Fatal("sidecar should depend on auth")
	}
	if dep.Condition != "service_started" {
		t.Errorf("expected condition service_started, got %s", dep.Condition)
	}

	if sidecar.Environment["URL"] != "s3://bucket/db" {
		t.Errorf("unexpected sidecar env: %v", sidecar.Environment)
	}
}

func TestGenerateInitContainerDependsOn(t *testing.T) {
	apps := []*config.AppConfig{
		{
			Name:  "auth",
			Image: "ghcr.io/org/auth",
			Init: []config.InitContainerConfig{
				{
					Name:    "restore",
					Image:   "litestream/litestream:0.5.6",
					Command: "litestream restore /data/auth.db",
					Volumes: []string{"auth-data:/data"},
				},
			},
		},
	}

	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	project := NewProject("test", apps, node, config.NetworkingConfig{})

	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	// Init container should exist with restart: no
	initSvc, ok := compose.Services["auth-restore"]
	if !ok {
		t.Fatal("auth-restore init service not found")
	}
	if initSvc.Restart != "no" {
		t.Errorf("expected init restart 'no', got %s", initSvc.Restart)
	}
	if initSvc.Command != "litestream restore /data/auth.db" {
		t.Errorf("unexpected init command: %s", initSvc.Command)
	}

	// Main app should depend on init container
	mainSvc, ok := compose.Services["auth"]
	if !ok {
		t.Fatal("auth service not found")
	}
	dep, ok := mainSvc.DependsOn["auth-restore"]
	if !ok {
		t.Fatal("auth should depend on auth-restore")
	}
	if dep.Condition != "service_completed_successfully" {
		t.Errorf("expected condition service_completed_successfully, got %s", dep.Condition)
	}
}

func TestGenerateVolumeDeduplication(t *testing.T) {
	apps := []*config.AppConfig{
		{
			Name:  "auth",
			Image: "ghcr.io/org/auth",
			Volumes: []config.VolumeConfig{
				{Name: "auth-data", Path: "/data"},
			},
			Sidecars: []config.SidecarConfig{
				{
					Name:    "litestream",
					Image:   "litestream:latest",
					Volumes: []string{"auth-data:/data"},
				},
			},
			Init: []config.InitContainerConfig{
				{
					Name:    "restore",
					Image:   "litestream:latest",
					Volumes: []string{"auth-data:/data"},
				},
			},
		},
	}

	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	project := NewProject("test", apps, node, config.NetworkingConfig{})

	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	if len(compose.Volumes) != 1 {
		t.Errorf("expected 1 deduplicated volume, got %d", len(compose.Volumes))
	}
	if _, ok := compose.Volumes["auth-data"]; !ok {
		t.Error("auth-data volume not found")
	}
}

func TestGenerateTraefikLabels(t *testing.T) {
	apps := []*config.AppConfig{
		{
			Name:    "api",
			Image:   "ghcr.io/org/api",
			Domains: []string{"api.example.com"},
			Ports:   []config.PortConfig{{Container: 3000}},
		},
	}

	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	networking := config.NetworkingConfig{
		Traefik: config.TraefikConfig{
			EntryPoints: []string{"web", "websecure"},
			ACME: config.ACMEConfig{
				Enabled:  true,
				Provider: "letsencrypt",
			},
		},
	}

	project := NewProject("test", apps, node, networking)
	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	svc := compose.Services["api"]
	if svc.Labels["traefik.enable"] != "true" {
		t.Error("traefik.enable should be true")
	}

	routerRule := svc.Labels["traefik.http.routers.api-node-1.rule"]
	if !strings.Contains(routerRule, "api.example.com") {
		t.Errorf("expected router rule with domain, got %s", routerRule)
	}

	tlsLabel := svc.Labels["traefik.http.routers.api-node-1.tls"]
	if tlsLabel != "true" {
		t.Error("TLS should be enabled when ACME is enabled")
	}
}

func TestGenerateNoDomainsNoTraefikLabels(t *testing.T) {
	apps := []*config.AppConfig{
		{
			Name:  "worker",
			Image: "ghcr.io/org/worker",
		},
	}

	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	project := NewProject("test", apps, node, config.NetworkingConfig{})

	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	svc := compose.Services["worker"]
	if len(svc.Labels) != 0 {
		t.Errorf("expected no labels for service without domains, got %v", svc.Labels)
	}
}

func TestGenerateDefaultVersion(t *testing.T) {
	apps := []*config.AppConfig{
		{Name: "app", Image: "ghcr.io/org/app"},
	}

	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	project := NewProject("test", apps, node, config.NetworkingConfig{})

	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	if compose.Services["app"].Image != "ghcr.io/org/app:latest" {
		t.Errorf("expected default tag latest, got %s", compose.Services["app"].Image)
	}
}

func TestGenerateEmptyApps(t *testing.T) {
	node := &config.NodeConfig{ID: "node-1", Host: "10.0.0.1"}
	project := NewProject("test", []*config.AppConfig{}, node, config.NetworkingConfig{})

	output, err := project.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	if len(compose.Services) != 0 {
		t.Errorf("expected 0 services for empty apps, got %d", len(compose.Services))
	}

	if _, ok := compose.Networks["warpgate"]; !ok {
		t.Error("warpgate network should always be present")
	}
}

func TestGeneratePortFormats(t *testing.T) {
	tests := []struct {
		name     string
		port     config.PortConfig
		expected string
	}{
		{
			name:     "container only",
			port:     config.PortConfig{Container: 8080},
			expected: "8080",
		},
		{
			name:     "host and container",
			port:     config.PortConfig{Container: 8080, Host: 9090},
			expected: "9090:8080",
		},
		{
			name:     "udp protocol",
			port:     config.PortConfig{Container: 53, Protocol: "udp"},
			expected: "53/udp",
		},
		{
			name:     "tcp protocol omitted",
			port:     config.PortConfig{Container: 80, Protocol: "tcp"},
			expected: "80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps := []*config.AppConfig{
				{Name: "app", Image: "img", Ports: []config.PortConfig{tt.port}},
			}
			node := &config.NodeConfig{ID: "n", Host: "h"}
			project := NewProject("test", apps, node, config.NetworkingConfig{})

			output, err := project.Generate()
			if err != nil {
				t.Fatalf("Generate() error: %v", err)
			}

			var compose ComposeFile
			if err := yaml.Unmarshal([]byte(output), &compose); err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			svc := compose.Services["app"]
			if len(svc.Ports) != 1 || svc.Ports[0] != tt.expected {
				t.Errorf("expected port %q, got %v", tt.expected, svc.Ports)
			}
		})
	}
}
