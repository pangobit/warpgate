package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetTargetNodes(t *testing.T) {
	nodes := []NodeConfig{
		{ID: "node-1", Host: "10.0.0.1"},
		{ID: "node-2", Host: "10.0.0.2"},
	}

	tests := []struct {
		name     string
		app      AppConfig
		expected []string
	}{
		{
			name:     "specific target",
			app:      AppConfig{Name: "auth", Targets: []string{"node-1"}},
			expected: []string{"node-1"},
		},
		{
			name:     "multiple targets",
			app:      AppConfig{Name: "api", Targets: []string{"node-1", "node-2"}},
			expected: []string{"node-1", "node-2"},
		},
		{
			name:     "empty targets means all nodes",
			app:      AppConfig{Name: "app"},
			expected: []string{"node-1", "node-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.app.GetTargetNodes(nodes)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d targets, got %d", len(tt.expected), len(got))
			}
			for i, id := range got {
				if id != tt.expected[i] {
					t.Errorf("expected %q at index %d, got %q", tt.expected[i], i, id)
				}
			}
		})
	}
}

func TestClusterValidate(t *testing.T) {
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
			name: "valid minimal config",
			config: ClusterConfig{
				Project: "test",
				Nodes:   []NodeConfig{{ID: "n", Host: "h"}},
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

func TestValidateApp(t *testing.T) {
	tests := []struct {
		name    string
		app     AppConfig
		wantErr bool
	}{
		{
			name:    "missing name",
			app:     AppConfig{Image: "img"},
			wantErr: true,
		},
		{
			name:    "missing image",
			app:     AppConfig{Name: "app"},
			wantErr: true,
		},
		{
			name:    "valid app",
			app:     AppConfig{Name: "app", Image: "ghcr.io/org/app"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateApp(&tt.app)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateApp() error = %v, wantErr %v", err, tt.wantErr)
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

func TestLoadAppConfig(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "myapp")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}

	yml := `image: ghcr.io/org/myapp
version: v1.0.0
targets: [node-1]
secrets_prefix: myapp/prod
port: 8080
expose:
  public:
    domains: [myapp.example.com]
  private:
    port: 8080
  internal:
    hostname: myapp.internal
`
	if err := os.WriteFile(filepath.Join(appDir, "app.yml"), []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	app, err := LoadAppConfig(appDir)
	if err != nil {
		t.Fatalf("LoadAppConfig() error: %v", err)
	}

	if app.Image != "ghcr.io/org/myapp" {
		t.Errorf("expected image ghcr.io/org/myapp, got %s", app.Image)
	}
	if app.Version != "v1.0.0" {
		t.Errorf("expected version v1.0.0, got %s", app.Version)
	}
	if len(app.Targets) != 1 || app.Targets[0] != "node-1" {
		t.Errorf("unexpected targets: %v", app.Targets)
	}
	expose := app.EffectiveExpose()
	if expose.Public == nil || len(expose.Public.Domains) != 1 || expose.Public.Domains[0] != "myapp.example.com" {
		t.Errorf("unexpected public expose: %+v", expose.Public)
	}
	if expose.Private == nil || expose.Private.Port != 8080 {
		t.Errorf("unexpected private expose: %+v", expose.Private)
	}
	if expose.Internal == nil || expose.Internal.Hostname != "myapp.internal" {
		t.Errorf("unexpected internal expose: %+v", expose.Internal)
	}
	if app.SecretsPrefix != "myapp/prod" {
		t.Errorf("expected secrets_prefix myapp/prod, got %s", app.SecretsPrefix)
	}
	if app.Port != 8080 {
		t.Errorf("expected port 8080, got %d", app.Port)
	}
}

func TestDiscoverApps(t *testing.T) {
	dir := t.TempDir()
	appsDir := filepath.Join(dir, "apps")

	for _, name := range []string{"auth", "api", "site"} {
		appDir := filepath.Join(appsDir, name)
		if err := os.MkdirAll(appDir, 0755); err != nil {
			t.Fatal(err)
		}
		yml := "image: ghcr.io/org/" + name + "\n"
		if err := os.WriteFile(filepath.Join(appDir, "app.yml"), []byte(yml), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Directory without app.yml should be skipped
	if err := os.MkdirAll(filepath.Join(appsDir, "no-config"), 0755); err != nil {
		t.Fatal(err)
	}

	apps, err := DiscoverApps(dir)
	if err != nil {
		t.Fatalf("DiscoverApps() error: %v", err)
	}

	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}

	// Should be sorted alphabetically
	expected := []string{"api", "auth", "site"}
	for i, app := range apps {
		if app.Name != expected[i] {
			t.Errorf("expected app %q at index %d, got %q", expected[i], i, app.Name)
		}
	}
}

func TestRepoConfigGetAppsForNode(t *testing.T) {
	repo := &RepoConfig{
		Cluster: &ClusterConfig{
			Nodes: []NodeConfig{
				{ID: "node-1", Host: "10.0.0.1"},
				{ID: "node-2", Host: "10.0.0.2"},
			},
		},
		Apps: []*AppConfig{
			{Name: "auth", Image: "auth", Targets: []string{"node-1"}},
			{Name: "api", Image: "api"},
			{Name: "admin", Image: "admin", Targets: []string{"node-2"}},
		},
	}

	node1Apps := repo.GetAppsForNode("node-1")
	if len(node1Apps) != 2 {
		t.Fatalf("expected 2 apps for node-1, got %d", len(node1Apps))
	}

	node2Apps := repo.GetAppsForNode("node-2")
	if len(node2Apps) != 2 {
		t.Fatalf("expected 2 apps for node-2, got %d", len(node2Apps))
	}

	node3Apps := repo.GetAppsForNode("node-3")
	if len(node3Apps) != 0 {
		t.Fatalf("expected 0 apps for node-3, got %d", len(node3Apps))
	}
}
