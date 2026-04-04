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
			name:    "name with shell injection",
			app:     AppConfig{Name: "app; rm -rf /", Image: "img"},
			wantErr: true,
		},
		{
			name:    "name with spaces",
			app:     AppConfig{Name: "my app", Image: "img"},
			wantErr: true,
		},
		{
			name:    "name with uppercase",
			app:     AppConfig{Name: "MyApp", Image: "img"},
			wantErr: true,
		},
		{
			name:    "name starting with hyphen",
			app:     AppConfig{Name: "-app", Image: "img"},
			wantErr: true,
		},
		{
			name:    "name ending with hyphen",
			app:     AppConfig{Name: "app-", Image: "img"},
			wantErr: true,
		},
		{
			name:    "valid hyphenated name",
			app:     AppConfig{Name: "my-app", Image: "img"},
			wantErr: false,
		},
		{
			name:    "valid single char name",
			app:     AppConfig{Name: "a", Image: "img"},
			wantErr: false,
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
		{
			name: "expose.public with no domains",
			app: AppConfig{
				Name: "app", Image: "img", Port: 8080,
				Expose: &ExposeConfig{Public: &PublicExpose{Domains: nil}},
			},
			wantErr: true,
		},
		{
			name: "expose.public without port",
			app: AppConfig{
				Name: "app", Image: "img",
				Expose: &ExposeConfig{Public: &PublicExpose{Domains: []string{"a.com"}}},
			},
			wantErr: true,
		},
		{
			name: "expose.private with zero port",
			app: AppConfig{
				Name: "app", Image: "img",
				Expose: &ExposeConfig{Private: &PrivateExpose{Port: 0}},
			},
			wantErr: true,
		},
		{
			name: "expose.internal with empty hostname",
			app: AppConfig{
				Name: "app", Image: "img", Port: 8080,
				Expose: &ExposeConfig{Internal: &InternalExpose{Hostname: ""}},
			},
			wantErr: true,
		},
		{
			name: "expose.internal without port",
			app: AppConfig{
				Name: "app", Image: "img",
				Expose: &ExposeConfig{Internal: &InternalExpose{Hostname: "a.internal"}},
			},
			wantErr: true,
		},
		{
			name: "valid full expose config",
			app: AppConfig{
				Name: "app", Image: "img", Port: 8080,
				Expose: &ExposeConfig{
					Public:   &PublicExpose{Domains: []string{"a.com"}},
					Private:  &PrivateExpose{Port: 8080},
					Internal: &InternalExpose{Hostname: "a.internal"},
				},
			},
			wantErr: false,
		},
		{
			name:    "no expose is valid",
			app:     AppConfig{Name: "app", Image: "img", Port: 8080},
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

func TestLoadAppConfigWithKind(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "myapp")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}

	yml := "kind: warpgate/app\nimage: ghcr.io/org/myapp\n"
	if err := os.WriteFile(filepath.Join(appDir, "app.yml"), []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	app, err := LoadAppConfig(appDir)
	if err != nil {
		t.Fatalf("LoadAppConfig() error: %v", err)
	}
	if app.Kind != "warpgate/app" {
		t.Errorf("expected kind %q, got %q", "warpgate/app", app.Kind)
	}
}

func TestDiscoverAppsKindFiltering(t *testing.T) {
	type appFixture struct {
		name string
		yml  string
	}

	tests := []struct {
		name         string
		apps         []appFixture
		wantNames    []string
		wantKinds    []string
	}{
		{
			name: "correct kind accepted",
			apps: []appFixture{
				{"myapp", "kind: warpgate/app\nimage: ghcr.io/org/myapp\n"},
			},
			wantNames: []string{"myapp"},
			wantKinds: []string{"warpgate/app"},
		},
		{
			name: "no kind accepted for backwards compatibility",
			apps: []appFixture{
				{"legacy", "image: ghcr.io/org/legacy\n"},
			},
			wantNames: []string{"legacy"},
			wantKinds: []string{""},
		},
		{
			name: "explicit empty kind accepted",
			apps: []appFixture{
				{"empty-kind", "kind: \"\"\nimage: ghcr.io/org/empty\n"},
			},
			wantNames: []string{"empty-kind"},
			wantKinds: []string{""},
		},
		{
			name: "foreign kind skipped",
			apps: []appFixture{
				{"helm-app", "kind: helm/chart\nimage: ghcr.io/org/helm\n"},
			},
			wantNames: []string{},
			wantKinds: []string{},
		},
		{
			name: "case sensitive comparison",
			apps: []appFixture{
				{"upper", "kind: WARPGATE/APP\nimage: ghcr.io/org/upper\n"},
			},
			wantNames: []string{},
			wantKinds: []string{},
		},
		{
			name: "trailing whitespace rejected",
			apps: []appFixture{
				{"ws-app", "kind: \"warpgate/app \"\nimage: ghcr.io/org/ws\n"},
			},
			wantNames: []string{},
			wantKinds: []string{},
		},
		{
			name: "mixed: keeps valid and legacy, skips foreign",
			apps: []appFixture{
				{"aaa-valid", "kind: warpgate/app\nimage: ghcr.io/org/valid\n"},
				{"bbb-legacy", "image: ghcr.io/org/legacy\n"},
				{"ccc-foreign", "kind: other/thing\nimage: ghcr.io/org/foreign\n"},
			},
			wantNames: []string{"aaa-valid", "bbb-legacy"},
			wantKinds: []string{"warpgate/app", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			appsDir := filepath.Join(dir, "apps")

			for _, a := range tt.apps {
				appDir := filepath.Join(appsDir, a.name)
				if err := os.MkdirAll(appDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(appDir, "app.yml"), []byte(a.yml), 0644); err != nil {
					t.Fatal(err)
				}
			}

			apps, err := DiscoverApps(dir)
			if err != nil {
				t.Fatalf("DiscoverApps() error: %v", err)
			}

			if len(apps) != len(tt.wantNames) {
				t.Fatalf("expected %d apps, got %d", len(tt.wantNames), len(apps))
			}

			for i, app := range apps {
				if app.Name != tt.wantNames[i] {
					t.Errorf("app[%d].Name = %q, want %q", i, app.Name, tt.wantNames[i])
				}
				if app.Kind != tt.wantKinds[i] {
					t.Errorf("app[%d].Kind = %q, want %q", i, app.Kind, tt.wantKinds[i])
				}
			}
		})
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
