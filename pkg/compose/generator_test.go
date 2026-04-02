package compose

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

func TestGenerateOverrideWithDomains(t *testing.T) {
	app := &config.AppConfig{
		Name:    "api",
		Image:   "ghcr.io/org/api",
		Version: "v2.0.0",
		Domains: []string{"api.example.com"},
		Port:    3000,
	}

	networking := &config.NetworkingConfig{
		Traefik: config.TraefikConfig{
			EntryPoints: []string{"web", "websecure"},
			ACME: config.ACMEConfig{
				Enabled:  true,
				Provider: "letsencrypt",
			},
		},
	}

	output, err := GenerateOverride(app, networking, nil)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	svc, ok := override.Services["api"]
	if !ok {
		t.Fatal("api service not found in override")
	}

	if svc.Image != "ghcr.io/org/api:v2.0.0" {
		t.Errorf("expected image ghcr.io/org/api:v2.0.0, got %s", svc.Image)
	}

	if svc.Labels["traefik.enable"] != "true" {
		t.Error("traefik.enable should be true")
	}

	routerRule := svc.Labels["traefik.http.routers.api.rule"]
	if !strings.Contains(routerRule, "api.example.com") {
		t.Errorf("expected router rule with domain, got %s", routerRule)
	}

	if svc.Labels["traefik.http.routers.api.tls"] != "true" {
		t.Error("TLS should be enabled when ACME is enabled")
	}

	if svc.Labels["traefik.http.routers.api.tls.certresolver"] != "letsencrypt" {
		t.Error("certresolver should be letsencrypt")
	}

	portLabel := svc.Labels["traefik.http.services.api.loadbalancer.server.port"]
	if portLabel != "3000" {
		t.Errorf("expected port 3000, got %s", portLabel)
	}

	if len(svc.Networks) != 1 || svc.Networks[0] != "warpgate" {
		t.Errorf("expected warpgate network, got %v", svc.Networks)
	}

	net, ok := override.Networks["warpgate"]
	if !ok {
		t.Error("warpgate network not found")
	}
	if !net.External {
		t.Error("warpgate network should be external")
	}
}

func TestGenerateOverrideNoDomains(t *testing.T) {
	app := &config.AppConfig{
		Name:    "worker",
		Image:   "ghcr.io/org/worker",
		Version: "v1.0.0",
	}

	networking := &config.NetworkingConfig{}

	output, err := GenerateOverride(app, networking, nil)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse generated YAML: %v", err)
	}

	svc := override.Services["worker"]
	if len(svc.Labels) != 0 {
		t.Errorf("expected no labels for app without domains, got %v", svc.Labels)
	}

	if svc.Image != "ghcr.io/org/worker:v1.0.0" {
		t.Errorf("expected image ghcr.io/org/worker:v1.0.0, got %s", svc.Image)
	}
}

func TestGenerateOverrideDefaultVersion(t *testing.T) {
	app := &config.AppConfig{
		Name:  "app",
		Image: "ghcr.io/org/app",
	}

	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if override.Services["app"].Image != "ghcr.io/org/app:latest" {
		t.Errorf("expected default tag latest, got %s", override.Services["app"].Image)
	}
}

func TestGenerateOverrideMultipleDomains(t *testing.T) {
	app := &config.AppConfig{
		Name:    "site",
		Image:   "ghcr.io/org/site",
		Version: "v1.0.0",
		Domains: []string{"example.com", "www.example.com"},
		Port:    80,
	}

	networking := &config.NetworkingConfig{
		Traefik: config.TraefikConfig{
			EntryPoints: []string{"web", "websecure"},
			ACME: config.ACMEConfig{
				Enabled:  true,
				Provider: "letsencrypt",
			},
		},
	}

	output, err := GenerateOverride(app, networking, nil)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	svc := override.Services["site"]

	// First domain uses app name as router name
	if !strings.Contains(svc.Labels["traefik.http.routers.site.rule"], "example.com") {
		t.Error("first domain router rule missing")
	}

	// Second domain uses suffix
	if !strings.Contains(svc.Labels["traefik.http.routers.site-1.rule"], "www.example.com") {
		t.Error("second domain router rule missing")
	}
}

func TestGenerateOverrideInternalLabels(t *testing.T) {
	app := &config.AppConfig{
		Name:     "auth",
		Image:    "ghcr.io/org/auth",
		Version:  "v1.0.0",
		Internal: "auth.internal",
		Port:     8085,
	}

	networking := &config.NetworkingConfig{}
	internalHosts := []string{"auth.internal", "api.internal"}

	output, err := GenerateOverride(app, networking, internalHosts)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	svc := override.Services["auth"]

	routerRule := svc.Labels["traefik.http.routers.auth-internal.rule"]
	if routerRule != "Host(`auth.internal`)" {
		t.Errorf("expected internal router rule, got %q", routerRule)
	}

	ep := svc.Labels["traefik.http.routers.auth-internal.entrypoints"]
	if ep != "internal" {
		t.Errorf("expected internal entrypoint, got %q", ep)
	}

	if len(svc.ExtraHosts) != 2 {
		t.Fatalf("expected 2 extra_hosts, got %d", len(svc.ExtraHosts))
	}
	if svc.ExtraHosts[0] != "auth.internal:host-gateway" {
		t.Errorf("expected auth.internal:host-gateway, got %q", svc.ExtraHosts[0])
	}
	if svc.ExtraHosts[1] != "api.internal:host-gateway" {
		t.Errorf("expected api.internal:host-gateway, got %q", svc.ExtraHosts[1])
	}
}

func TestGenerateOverrideNoInternalNoExtraHosts(t *testing.T) {
	app := &config.AppConfig{
		Name:    "worker",
		Image:   "ghcr.io/org/worker",
		Version: "v1.0.0",
	}

	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(override.Services["worker"].ExtraHosts) != 0 {
		t.Errorf("expected no extra_hosts, got %v", override.Services["worker"].ExtraHosts)
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

	if !strings.Contains(output, "traefik:v3.4") {
		t.Error("expected traefik:v3.4 image")
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
	if !strings.Contains(output, "8080:8080") {
		t.Error("expected internal entrypoint port 8080")
	}
	if !strings.Contains(output, "providers.file.directory") {
		t.Error("expected file provider config")
	}
	if !strings.Contains(output, "/opt/warpgate/traefik/dynamic") {
		t.Error("expected dynamic config volume mount")
	}
}
