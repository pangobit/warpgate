package deploy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestGenerateShadowInternalRoute(t *testing.T) {
	tests := []struct {
		name    string
		app     *config.AppConfig
		cluster *config.ClusterConfig
		check   func(t *testing.T, output string)
	}{
		{
			name: "generates shadow hostname and backends for target nodes",
			app: &config.AppConfig{
				Name:    "auth",
				Image:   "ghcr.io/org/auth",
				Port:    8085,
				Targets: []string{"node-1", "node-2"},
				Expose: &config.ExposeConfig{
					Internal: &config.InternalExpose{Hostname: "auth.internal"},
				},
			},
			cluster: &config.ClusterConfig{
				Nodes: []config.NodeConfig{
					{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
					{ID: "node-2", Host: "10.0.0.2", PrivateIP: "100.95.115.82"},
				},
			},
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "shadow-auth.internal") {
					t.Error("expected Host rule with shadow-auth.internal")
				}
				if !strings.Contains(output, "100.95.115.81:8085") {
					t.Error("expected node-1 private IP as backend")
				}
				if !strings.Contains(output, "100.95.115.82:8085") {
					t.Error("expected node-2 private IP as backend")
				}
				if !strings.Contains(output, "auth-shadow-internal") {
					t.Error("expected auth-shadow-internal router name")
				}
			},
		},
		{
			name: "returns empty when app has no internal expose",
			app: &config.AppConfig{
				Name:  "worker",
				Image: "ghcr.io/org/worker",
				Port:  8080,
			},
			cluster: &config.ClusterConfig{
				Nodes: []config.NodeConfig{
					{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
				},
			},
			check: func(t *testing.T, output string) {
				if output != "" {
					t.Errorf("expected empty output, got %q", output)
				}
			},
		},
		{
			name: "returns empty when port is zero",
			app: &config.AppConfig{
				Name: "auth",
				Port: 0,
				Expose: &config.ExposeConfig{
					Internal: &config.InternalExpose{Hostname: "auth.internal"},
				},
			},
			cluster: &config.ClusterConfig{
				Nodes: []config.NodeConfig{
					{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
				},
			},
			check: func(t *testing.T, output string) {
				if output != "" {
					t.Errorf("expected empty output, got %q", output)
				}
			},
		},
		{
			name: "skips nodes without private IP",
			app: &config.AppConfig{
				Name:    "auth",
				Image:   "ghcr.io/org/auth",
				Port:    8085,
				Targets: []string{"node-1", "node-2"},
				Expose: &config.ExposeConfig{
					Internal: &config.InternalExpose{Hostname: "auth.internal"},
				},
			},
			cluster: &config.ClusterConfig{
				Nodes: []config.NodeConfig{
					{ID: "node-1", Host: "10.0.0.1", PrivateIP: "100.95.115.81"},
					{ID: "node-2", Host: "10.0.0.2"},
				},
			},
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "100.95.115.81") {
					t.Error("expected node-1 with private IP")
				}
				if strings.Contains(output, "10.0.0.2") {
					t.Error("node-2 without private IP should be skipped")
				}
			},
		},
		{
			name: "returns empty when all nodes lack private IP",
			app: &config.AppConfig{
				Name:    "auth",
				Image:   "ghcr.io/org/auth",
				Port:    8085,
				Targets: []string{"node-1"},
				Expose: &config.ExposeConfig{
					Internal: &config.InternalExpose{Hostname: "auth.internal"},
				},
			},
			cluster: &config.ClusterConfig{
				Nodes: []config.NodeConfig{
					{ID: "node-1", Host: "10.0.0.1"},
				},
			},
			check: func(t *testing.T, output string) {
				if output != "" {
					t.Errorf("expected empty output when no node has private IP, got %q", output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := GenerateShadowInternalRoute(tt.app, tt.cluster)
			if err != nil {
				t.Fatalf("GenerateShadowInternalRoute() error: %v", err)
			}
			tt.check(t, output)
		})
	}
}

func TestDeployStateShadowRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	state := &DeployState{
		App:              "client",
		CurrentVersion:   "v1.0.0",
		PreviousVersion:  "v0.9.0",
		ActiveSlot:       "blue",
		DeployedAt:       now,
		ShadowVersion:    "v2.0.0",
		ShadowDeployedAt: &now,
	}

	data, err := state.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	parsed, err := UnmarshalState(data)
	if err != nil {
		t.Fatalf("UnmarshalState() error: %v", err)
	}

	if parsed.ShadowVersion != "v2.0.0" {
		t.Errorf("expected shadow version v2.0.0, got %s", parsed.ShadowVersion)
	}
	if parsed.ShadowDeployedAt == nil {
		t.Fatal("expected shadow deployed_at to be non-nil")
	}
	if parsed.CurrentVersion != "v1.0.0" {
		t.Errorf("shadow fields should not corrupt live fields: expected v1.0.0, got %s", parsed.CurrentVersion)
	}
	if parsed.ActiveSlot != "blue" {
		t.Errorf("shadow fields should not corrupt live fields: expected blue, got %s", parsed.ActiveSlot)
	}
}

func TestDeployStateShadowOmitsEmptyFields(t *testing.T) {
	state := &DeployState{
		App:            "worker",
		CurrentVersion: "v1.0.0",
		ActiveSlot:     "blue",
		DeployedAt:     time.Now(),
	}

	data, err := state.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	for _, key := range []string{"shadow_version", "shadow_deployed_at"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %s to be omitted from JSON when empty", key)
		}
	}
}
