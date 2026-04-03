package compose

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

const singleServiceCompose = `services:
  api:
    image: ghcr.io/org/api
    ports: ["3000:3000"]
`

func TestGenerateOverrideWithDomains(t *testing.T) {
	app := &config.AppConfig{
		Name:    "api",
		Image:   "ghcr.io/org/api",
		Version: "v2.0.0",
		Port:    3000,
		Expose: &config.ExposeConfig{
			Public: &config.PublicExpose{Domains: []string{"api.example.com"}},
		},
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

	output, err := GenerateOverride(app, networking, nil, singleServiceCompose)
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

	netCfg, ok := svc.Networks["warpgate"]
	if !ok {
		t.Fatal("warpgate network not found on service")
	}
	if len(netCfg.Aliases) != 1 || netCfg.Aliases[0] != "api" {
		t.Errorf("expected alias [api], got %v", netCfg.Aliases)
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

	compose := `services:
  worker:
    image: ghcr.io/org/worker
`
	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil, compose)
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

	compose := `services:
  app:
    image: ghcr.io/org/app
`
	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil, compose)
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
		Port:    80,
		Expose: &config.ExposeConfig{
			Public: &config.PublicExpose{Domains: []string{"example.com", "www.example.com"}},
		},
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

	compose := `services:
  site:
    image: ghcr.io/org/site
`
	output, err := GenerateOverride(app, networking, nil, compose)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	svc := override.Services["site"]

	if !strings.Contains(svc.Labels["traefik.http.routers.site.rule"], "example.com") {
		t.Error("first domain router rule missing")
	}

	if !strings.Contains(svc.Labels["traefik.http.routers.site-1.rule"], "www.example.com") {
		t.Error("second domain router rule missing")
	}
}

func TestGenerateOverrideInternalLabels(t *testing.T) {
	app := &config.AppConfig{
		Name:    "auth",
		Image:   "ghcr.io/org/auth",
		Version: "v1.0.0",
		Port:    8085,
		Expose: &config.ExposeConfig{
			Private:  &config.PrivateExpose{Port: 8085},
			Internal: &config.InternalExpose{Hostname: "auth.internal"},
		},
	}

	networking := &config.NetworkingConfig{}
	internalHosts := []string{"auth.internal", "api.internal"}

	compose := `services:
  auth:
    image: ghcr.io/org/auth
`
	output, err := GenerateOverride(app, networking, internalHosts, compose)
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

	portEP := svc.Labels["traefik.http.routers.auth-port-internal.entrypoints"]
	if portEP != "auth-port-internal" {
		t.Errorf("expected port entrypoint auth-port-internal, got %q", portEP)
	}

	portRule := svc.Labels["traefik.http.routers.auth-port-internal.rule"]
	if portRule != "PathPrefix(`/`)" {
		t.Errorf("expected port router rule PathPrefix(`/`), got %q", portRule)
	}
}

func TestGenerateOverrideNoInternalNoExtraHosts(t *testing.T) {
	app := &config.AppConfig{
		Name:    "worker",
		Image:   "ghcr.io/org/worker",
		Version: "v1.0.0",
	}

	compose := `services:
  worker:
    image: ghcr.io/org/worker
`
	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil, compose)
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

func TestGenerateOverrideAllServicesGetNetwork(t *testing.T) {
	app := &config.AppConfig{
		Name:    "brighter-platform",
		Image:   "ghcr.io/org/client",
		Version: "v1.0.0",
		Port:    8083,
		Expose: &config.ExposeConfig{
			Public: &config.PublicExpose{Domains: []string{"example.com"}},
		},
		Sidecars: map[string]config.SidecarConfig{
			"admin": {
				Port: 8087,
				Expose: &config.ExposeConfig{
					Private: &config.PrivateExpose{Port: 8087},
				},
			},
		},
	}

	compose := `services:
  brighter-platform:
    image: ghcr.io/org/client
  admin:
    image: ghcr.io/org/admin
`
	output, err := GenerateOverride(app, &config.NetworkingConfig{
		Traefik: config.TraefikConfig{EntryPoints: []string{"web"}},
	}, nil, compose)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	for _, svcName := range []string{"brighter-platform", "admin"} {
		svc, ok := override.Services[svcName]
		if !ok {
			t.Fatalf("service %s not found", svcName)
		}
		netCfg, ok := svc.Networks["warpgate"]
		if !ok {
			t.Fatalf("service %s: warpgate network not found", svcName)
		}
		if len(netCfg.Aliases) != 1 || netCfg.Aliases[0] != svcName {
			t.Errorf("service %s: expected alias [%s], got %v", svcName, svcName, netCfg.Aliases)
		}
	}
}

func TestGenerateOverrideNetworkAliases(t *testing.T) {
	app := &config.AppConfig{
		Name:    "auth",
		Image:   "ghcr.io/org/auth",
		Version: "v1.0.0",
	}

	compose := `services:
  auth:
    image: ghcr.io/org/auth
  litestream:
    image: litestream/litestream
`
	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil, compose)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	for _, svcName := range []string{"auth", "litestream"} {
		svc, ok := override.Services[svcName]
		if !ok {
			t.Fatalf("service %s not found", svcName)
		}
		netCfg, ok := svc.Networks["warpgate"]
		if !ok {
			t.Fatalf("service %s: warpgate network not found", svcName)
		}
		if len(netCfg.Aliases) != 1 || netCfg.Aliases[0] != svcName {
			t.Errorf("service %s: expected alias [%s], got %v", svcName, svcName, netCfg.Aliases)
		}
	}

	// litestream should not have image or labels (not the main service, not a declared sidecar)
	ls := override.Services["litestream"]
	if ls.Image != "" {
		t.Errorf("litestream should not have image override, got %s", ls.Image)
	}
	if len(ls.Labels) != 0 {
		t.Errorf("litestream should not have labels, got %v", ls.Labels)
	}
}

func TestGenerateOverrideSidecarLabels(t *testing.T) {
	app := &config.AppConfig{
		Name:    "brighter-platform",
		Image:   "ghcr.io/org/client",
		Version: "v1.0.0",
		Port:    8083,
		Sidecars: map[string]config.SidecarConfig{
			"admin": {
				Port: 8087,
				Expose: &config.ExposeConfig{
					Private: &config.PrivateExpose{Port: 8087},
				},
			},
		},
	}

	compose := `services:
  brighter-platform:
    image: ghcr.io/org/client
  admin:
    image: ghcr.io/org/admin
`
	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil, compose)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	admin := override.Services["admin"]
	if admin.Labels["traefik.enable"] != "true" {
		t.Error("admin sidecar should have traefik.enable=true")
	}

	routerName := "brighter-platform-admin-internal"
	ep := admin.Labels["traefik.http.routers."+routerName+".entrypoints"]
	if ep != routerName {
		t.Errorf("expected entrypoint %s, got %s", routerName, ep)
	}

	portLabel := admin.Labels["traefik.http.services."+routerName+".loadbalancer.server.port"]
	if portLabel != "8087" {
		t.Errorf("expected sidecar port 8087, got %s", portLabel)
	}
}

func TestGenerateOverrideMainServiceInternalPortLabels(t *testing.T) {
	app := &config.AppConfig{
		Name:    "brighter-platform",
		Image:   "ghcr.io/org/client",
		Version: "v1.0.0",
		Port:    8083,
		Expose: &config.ExposeConfig{
			Private: &config.PrivateExpose{Port: 8083},
		},
	}

	compose := `services:
  brighter-platform:
    image: ghcr.io/org/client
`
	output, err := GenerateOverride(app, &config.NetworkingConfig{}, nil, compose)
	if err != nil {
		t.Fatalf("GenerateOverride() error: %v", err)
	}

	var override OverrideFile
	if err := yaml.Unmarshal([]byte(output), &override); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	main := override.Services["brighter-platform"]
	routerName := "brighter-platform-port-internal"
	ep := main.Labels["traefik.http.routers."+routerName+".entrypoints"]
	if ep != routerName {
		t.Errorf("expected entrypoint %s, got %s", routerName, ep)
	}

	rule := main.Labels["traefik.http.routers."+routerName+".rule"]
	if rule != "PathPrefix(`/`)" {
		t.Errorf("expected PathPrefix(`/`) rule, got %s", rule)
	}

	portLabel := main.Labels["traefik.http.services."+routerName+".loadbalancer.server.port"]
	if portLabel != "8083" {
		t.Errorf("expected app port 8083, got %s", portLabel)
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
	// Internal entrypoint moved to internal proxy
	if strings.Contains(output, "8080:8080") {
		t.Error("public Traefik should not have internal entrypoint 8080")
	}
	if strings.Contains(output, "providers.file.directory") {
		t.Error("public Traefik should not have file provider (moved to internal proxy)")
	}
}
