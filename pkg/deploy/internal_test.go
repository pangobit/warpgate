package deploy

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func internalRouteApp(targets []string, port int, hostname string) *config.AppConfig {
	app := &config.AppConfig{
		Name:    "auth",
		Targets: targets,
		Release: config.ReleaseConfig{
			Services: map[string]config.ReleaseServiceConfig{
				"auth": {
					Image: "ghcr.io/org/auth",
					Port:  port,
					Expose: &config.ExposeConfig{
						Internal: &config.InternalExpose{Hostname: hostname},
					},
				},
			},
		},
	}
	return app
}

func TestGenerateReleaseServiceInternalRoute(t *testing.T) {
	app := internalRouteApp([]string{"node-1", "node-2"}, 8085, "auth.internal")
	service := app.Release.Services["auth"]

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
			{ID: "node-2", Host: "10.0.0.2", PrivateIP: "100.95.115.82"},
			{ID: "node-3", Host: "10.0.0.3", PrivateIP: "100.95.115.83"},
		},
	}

	output, err := GenerateReleaseServiceInternalRoute(app, "auth", service, cluster)
	if err != nil {
		t.Fatalf("GenerateReleaseServiceInternalRoute() error: %v", err)
	}

	if !strings.Contains(output, "auth.internal") {
		t.Error("expected Host rule with auth.internal")
	}
	if !strings.Contains(output, "100.95.115.81:8085") {
		t.Error("expected node-1 private IP as backend")
	}
	if !strings.Contains(output, "100.95.115.82:8085") {
		t.Error("expected node-2 private IP as backend")
	}
	if strings.Contains(output, "100.95.115.83") {
		t.Error("node-3 should not be included (not a target)")
	}
	if !strings.Contains(output, "internal") {
		t.Error("expected internal entrypoint")
	}
}

func TestGenerateReleaseServiceInternalRouteNoInternal(t *testing.T) {
	app := &config.AppConfig{
		Name: "worker",
		Release: config.ReleaseConfig{Services: map[string]config.ReleaseServiceConfig{
			"worker": {Image: "ghcr.io/org/worker", Port: 8080},
		}},
	}
	service := app.Release.Services["worker"]

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
		},
	}

	output, err := GenerateReleaseServiceInternalRoute(app, "worker", service, cluster)
	if err != nil {
		t.Fatalf("GenerateReleaseServiceInternalRoute() error: %v", err)
	}

	if output != "" {
		t.Errorf("expected empty output for app without internal hostname, got %q", output)
	}
}

func TestGenerateReleaseServiceInternalRouteAllNodes(t *testing.T) {
	app := internalRouteApp(nil, 8085, "auth.internal")
	service := app.Release.Services["auth"]

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
			{ID: "node-2", Host: "10.0.0.2", PrivateIP: "100.95.115.82"},
		},
	}

	output, err := GenerateReleaseServiceInternalRoute(app, "auth", service, cluster)
	if err != nil {
		t.Fatalf("GenerateReleaseServiceInternalRoute() error: %v", err)
	}

	if !strings.Contains(output, "100.95.115.81") {
		t.Error("expected node-1 when targets empty (all nodes)")
	}
	if !strings.Contains(output, "100.95.115.82") {
		t.Error("expected node-2 when targets empty (all nodes)")
	}
}

func TestGenerateReleaseServiceInternalRouteSkipsNoPrivateIP(t *testing.T) {
	app := internalRouteApp([]string{"node-1", "node-2"}, 8085, "auth.internal")
	service := app.Release.Services["auth"]

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
			{ID: "node-2", Host: "10.0.0.2"},
		},
	}

	output, err := GenerateReleaseServiceInternalRoute(app, "auth", service, cluster)
	if err != nil {
		t.Fatalf("GenerateReleaseServiceInternalRoute() error: %v", err)
	}

	if !strings.Contains(output, "100.95.115.81") {
		t.Error("expected node-1 with private IP")
	}
	if strings.Contains(output, "10.0.0.2") {
		t.Error("node-2 without private IP should be skipped")
	}
}
