package deploy

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestGenerateInternalRoute(t *testing.T) {
	app := &config.AppConfig{
		Name:    "auth",
		Image:   "ghcr.io/org/auth",
		Port:    8085,
		Targets: []string{"node-1", "node-2"},
		Expose: &config.ExposeConfig{
			Internal: &config.InternalExpose{Hostname: "auth.internal"},
		},
	}

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
			{ID: "node-2", Host: "10.0.0.2", PrivateIP: "100.95.115.82"},
			{ID: "node-3", Host: "10.0.0.3", PrivateIP: "100.95.115.83"},
		},
	}

	output, err := GenerateInternalRoute(app, cluster)
	if err != nil {
		t.Fatalf("GenerateInternalRoute() error: %v", err)
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

func TestGenerateInternalRouteNoInternal(t *testing.T) {
	app := &config.AppConfig{
		Name:  "worker",
		Image: "ghcr.io/org/worker",
		Port:  8080,
	}

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
		},
	}

	output, err := GenerateInternalRoute(app, cluster)
	if err != nil {
		t.Fatalf("GenerateInternalRoute() error: %v", err)
	}

	if output != "" {
		t.Errorf("expected empty output for app without internal hostname, got %q", output)
	}
}

func TestGenerateInternalRouteAllNodes(t *testing.T) {
	app := &config.AppConfig{
		Name:  "auth",
		Image: "ghcr.io/org/auth",
		Port:  8085,
		Expose: &config.ExposeConfig{
			Internal: &config.InternalExpose{Hostname: "auth.internal"},
		},
	}

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
			{ID: "node-2", Host: "10.0.0.2", PrivateIP: "100.95.115.82"},
		},
	}

	output, err := GenerateInternalRoute(app, cluster)
	if err != nil {
		t.Fatalf("GenerateInternalRoute() error: %v", err)
	}

	if !strings.Contains(output, "100.95.115.81") {
		t.Error("expected node-1 when targets empty (all nodes)")
	}
	if !strings.Contains(output, "100.95.115.82") {
		t.Error("expected node-2 when targets empty (all nodes)")
	}
}

func TestGenerateInternalRouteSkipsNoPrivateIP(t *testing.T) {
	app := &config.AppConfig{
		Name:    "auth",
		Image:   "ghcr.io/org/auth",
		Port:    8085,
		Targets: []string{"node-1", "node-2"},
		Expose: &config.ExposeConfig{
			Internal: &config.InternalExpose{Hostname: "auth.internal"},
		},
	}

	cluster := &config.ClusterConfig{
		Nodes: []config.NodeConfig{
			{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
			{ID: "node-2", Host: "10.0.0.2"},
		},
	}

	output, err := GenerateInternalRoute(app, cluster)
	if err != nil {
		t.Fatalf("GenerateInternalRoute() error: %v", err)
	}

	if !strings.Contains(output, "100.95.115.81") {
		t.Error("expected node-1 with private IP")
	}
	if strings.Contains(output, "10.0.0.2") {
		t.Error("node-2 without private IP should be skipped")
	}
}
