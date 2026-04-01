package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGetAppsForNode(t *testing.T) {
	cfg := &ClusterConfig{
		Nodes: []NodeConfig{
			{ID: "node-1", Host: "10.0.0.1"},
			{ID: "node-2", Host: "10.0.0.2"},
		},
		Apps: []AppConfig{
			{Name: "auth", Image: "auth:latest", Targets: []string{"node-1"}},
			{Name: "api", Image: "api:latest", Targets: []string{"all"}},
			{Name: "admin", Image: "admin:latest", Targets: []string{"node-2"}},
			{Name: "app-no-targets", Image: "app:latest"},
		},
	}

	tests := []struct {
		name     string
		nodeID   string
		expected []string
	}{
		{
			name:     "node-1 gets auth, api, and app-no-targets",
			nodeID:   "node-1",
			expected: []string{"auth", "api", "app-no-targets"},
		},
		{
			name:     "node-2 gets api, admin, and app-no-targets",
			nodeID:   "node-2",
			expected: []string{"api", "admin", "app-no-targets"},
		},
		{
			name:     "unknown node gets nothing",
			nodeID:   "node-3",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps := cfg.GetAppsForNode(tt.nodeID)
			if len(apps) != len(tt.expected) {
				t.Fatalf("expected %d apps, got %d", len(tt.expected), len(apps))
			}
			for i, app := range apps {
				if app.Name != tt.expected[i] {
					t.Errorf("expected app %q at index %d, got %q", tt.expected[i], i, app.Name)
				}
			}
		})
	}
}

func TestSidecarConfigParsing(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		wantSidecar int
		wantInit    int
	}{
		{
			name: "valid sidecars and init",
			input: `
name: test-app
image: ghcr.io/org/app
sidecars:
  - name: litestream
    image: litestream/litestream:0.5.6
    volumes: [app-data:/data]
    env:
      LITESTREAM_URL: s3://bucket/db
init:
  - name: restore
    image: litestream/litestream:0.5.6
    command: "litestream restore /data/app.db"
    volumes: [app-data:/data]
`,
			wantErr:     false,
			wantSidecar: 1,
			wantInit:    1,
		},
		{
			name: "no sidecars or init",
			input: `
name: simple-app
image: ghcr.io/org/app`,
			wantErr:     false,
			wantSidecar: 0,
			wantInit:    0,
		},
		{
			name:    "invalid yaml",
			input:   `sidecars: [[[invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var app AppConfig
			err := yaml.Unmarshal([]byte(tt.input), &app)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(app.Sidecars) != tt.wantSidecar {
				t.Errorf("expected %d sidecars, got %d", tt.wantSidecar, len(app.Sidecars))
			}
			if len(app.Init) != tt.wantInit {
				t.Errorf("expected %d init containers, got %d", tt.wantInit, len(app.Init))
			}
		})
	}
}

func TestSidecarConfigFields(t *testing.T) {
	input := `
name: test-app
image: ghcr.io/org/app
sidecars:
  - name: litestream
    image: litestream/litestream:0.5.6
    volumes: [app-data:/data]
    env:
      LITESTREAM_URL: s3://bucket/db
init:
  - name: restore
    image: litestream/litestream:0.5.6
    command: "litestream restore /data/app.db"
    volumes: [app-data:/data]
`
	var app AppConfig
	if err := yaml.Unmarshal([]byte(input), &app); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	sc := app.Sidecars[0]
	if sc.Name != "litestream" {
		t.Errorf("expected sidecar name litestream, got %s", sc.Name)
	}
	if sc.Image != "litestream/litestream:0.5.6" {
		t.Errorf("expected sidecar image litestream/litestream:0.5.6, got %s", sc.Image)
	}
	if len(sc.Volumes) != 1 || sc.Volumes[0] != "app-data:/data" {
		t.Errorf("unexpected sidecar volumes: %v", sc.Volumes)
	}
	if sc.Env["LITESTREAM_URL"] != "s3://bucket/db" {
		t.Errorf("unexpected sidecar env: %v", sc.Env)
	}

	init := app.Init[0]
	if init.Name != "restore" {
		t.Errorf("expected init name restore, got %s", init.Name)
	}
	if init.Command != "litestream restore /data/app.db" {
		t.Errorf("expected init command, got %s", init.Command)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ClusterConfig
		wantErr bool
	}{
		{
			name:    "empty project name",
			config:  ClusterConfig{Nodes: []NodeConfig{{ID: "n", Host: "h"}}},
			wantErr: true,
		},
		{
			name:    "no nodes",
			config:  ClusterConfig{Project: "test"},
			wantErr: true,
		},
		{
			name: "node missing id",
			config: ClusterConfig{
				Project: "test",
				Nodes:   []NodeConfig{{Host: "h"}},
			},
			wantErr: true,
		},
		{
			name: "node missing host",
			config: ClusterConfig{
				Project: "test",
				Nodes:   []NodeConfig{{ID: "n"}},
			},
			wantErr: true,
		},
		{
			name: "app missing name",
			config: ClusterConfig{
				Project: "test",
				Nodes:   []NodeConfig{{ID: "n", Host: "h"}},
				Apps:    []AppConfig{{Image: "img"}},
			},
			wantErr: true,
		},
		{
			name: "app missing image",
			config: ClusterConfig{
				Project: "test",
				Nodes:   []NodeConfig{{ID: "n", Host: "h"}},
				Apps:    []AppConfig{{Name: "a"}},
			},
			wantErr: true,
		},
		{
			name: "valid minimal config",
			config: ClusterConfig{
				Project: "test",
				Nodes:   []NodeConfig{{ID: "n", Host: "h"}},
			},
			wantErr: false,
		},
		{
			name: "valid config with apps",
			config: ClusterConfig{
				Project: "test",
				Nodes:   []NodeConfig{{ID: "n", Host: "h"}},
				Apps:    []AppConfig{{Name: "a", Image: "img"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("TEST_VAR", "hello")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple expansion",
			input:    "${TEST_VAR}",
			expected: "hello",
		},
		{
			name:     "default value used",
			input:    "${UNSET_VAR:-fallback}",
			expected: "fallback",
		},
		{
			name:     "default value not used when set",
			input:    "${TEST_VAR:-fallback}",
			expected: "hello",
		},
		{
			name:     "no expansion needed",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
