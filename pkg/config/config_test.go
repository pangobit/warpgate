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

func TestReleaseServiceConfigEffectiveImageRef(t *testing.T) {
	tests := []struct {
		name         string
		service      ReleaseServiceConfig
		wantImageTag string
		wantImageRef string
	}{
		{
			name: "image tag",
			service: ReleaseServiceConfig{
				Image:    "ghcr.io/org/app",
				ImageTag: "v2.0.0",
			},
			wantImageTag: "v2.0.0",
			wantImageRef: "ghcr.io/org/app:v2.0.0",
		},
		{
			name: "digest image ref",
			service: ReleaseServiceConfig{
				Image:       "ghcr.io/org/app",
				ImageTag:    "v2.0.0",
				ImageDigest: "sha256:abc123",
			},
			wantImageTag: "v2.0.0",
			wantImageRef: "ghcr.io/org/app@sha256:abc123",
		},
		{
			name: "empty defaults to latest",
			service: ReleaseServiceConfig{
				Image: "ghcr.io/org/app",
			},
			wantImageTag: "latest",
			wantImageRef: "ghcr.io/org/app:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.service.EffectiveImageTag(); got != tt.wantImageTag {
				t.Errorf("EffectiveImageTag() = %q, want %q", got, tt.wantImageTag)
			}
			if got := tt.service.EffectiveImageRef(); got != tt.wantImageRef {
				t.Errorf("EffectiveImageRef() = %q, want %q", got, tt.wantImageRef)
			}
		})
	}
}

func TestAppConfigEffectiveReleaseServices(t *testing.T) {
	t.Run("copies release services without reading top-level image fields", func(t *testing.T) {
		app := &AppConfig{
			Name:  "bundle",
			Image: "ghcr.io/org/legacy",
			Release: ReleaseConfig{
				Services: map[string]ReleaseServiceConfig{
					"api": {
						Image:    "ghcr.io/org/api",
						ImageTag: "v2.0.0",
					},
					"admin": {
						Image:       "ghcr.io/org/admin",
						ImageDigest: "sha256:admin",
					},
				},
			},
		}

		services := app.EffectiveReleaseServices()
		if len(services) != 2 {
			t.Fatalf("services = %d, want 2", len(services))
		}
		if services["api"].EffectiveImageRef() != "ghcr.io/org/api:v2.0.0" {
			t.Errorf("api image ref = %q", services["api"].EffectiveImageRef())
		}
		if services["admin"].EffectiveImageRef() != "ghcr.io/org/admin@sha256:admin" {
			t.Errorf("admin image ref = %q", services["admin"].EffectiveImageRef())
		}
		services["api"] = ReleaseServiceConfig{Image: "mutated"}
		if app.Release.Services["api"].Image != "ghcr.io/org/api" {
			t.Errorf("EffectiveReleaseServices mutated app config: %q", app.Release.Services["api"].Image)
		}
	})
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

func validReleaseApp(name string) AppConfig {
	return AppConfig{
		Name: name,
		Release: ReleaseConfig{
			Services: map[string]ReleaseServiceConfig{
				name: {Image: "ghcr.io/org/" + name},
			},
		},
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
			app:     validReleaseApp(""),
			wantErr: true,
		},
		{
			name:    "name with shell injection",
			app:     validReleaseApp("app; rm -rf /"),
			wantErr: true,
		},
		{
			name:    "name with spaces",
			app:     validReleaseApp("my app"),
			wantErr: true,
		},
		{
			name:    "name with uppercase",
			app:     validReleaseApp("MyApp"),
			wantErr: true,
		},
		{
			name:    "name starting with hyphen",
			app:     validReleaseApp("-app"),
			wantErr: true,
		},
		{
			name:    "name ending with hyphen",
			app:     validReleaseApp("app-"),
			wantErr: true,
		},
		{
			name:    "valid hyphenated name",
			app:     validReleaseApp("my-app"),
			wantErr: false,
		},
		{
			name:    "valid single char name",
			app:     validReleaseApp("a"),
			wantErr: false,
		},
		{
			name:    "missing release services",
			app:     AppConfig{Name: "app"},
			wantErr: true,
		},
		{
			name: "top-level image is rejected",
			app: AppConfig{
				Name: "app", Image: "img",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{
					"app": {Image: "ghcr.io/org/app"},
				}},
			},
			wantErr: true,
		},
		{
			name: "release service missing image",
			app: AppConfig{
				Name:    "app",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{"app": {}}},
			},
			wantErr: true,
		},
		{
			name: "release service expose.public with no domains",
			app: AppConfig{
				Name: "app",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{
					"app": {
						Image:  "ghcr.io/org/app",
						Port:   8080,
						Expose: &ExposeConfig{Public: &PublicExpose{Domains: nil}},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "release service expose.public without port",
			app: AppConfig{
				Name: "app",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{
					"app": {
						Image:  "ghcr.io/org/app",
						Expose: &ExposeConfig{Public: &PublicExpose{Domains: []string{"a.com"}}},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "release service expose.private with zero port",
			app: AppConfig{
				Name: "app",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{
					"app": {
						Image:  "ghcr.io/org/app",
						Expose: &ExposeConfig{Private: &PrivateExpose{Port: 0}},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "release service expose.internal with empty hostname",
			app: AppConfig{
				Name: "app",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{
					"app": {
						Image:  "ghcr.io/org/app",
						Port:   8080,
						Expose: &ExposeConfig{Internal: &InternalExpose{Hostname: ""}},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "release service expose.internal without port",
			app: AppConfig{
				Name: "app",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{
					"app": {
						Image:  "ghcr.io/org/app",
						Expose: &ExposeConfig{Internal: &InternalExpose{Hostname: "a.internal"}},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "valid release service expose config",
			app: AppConfig{
				Name: "app",
				Release: ReleaseConfig{Services: map[string]ReleaseServiceConfig{
					"app": {
						Image: "ghcr.io/org/app",
						Port:  8080,
						Expose: &ExposeConfig{
							Public:   &PublicExpose{Domains: []string{"a.com"}},
							Private:  &PrivateExpose{Port: 8080},
							Internal: &InternalExpose{Hostname: "a.internal"},
						},
					},
				}},
			},
			wantErr: false,
		},
		{
			name: "persistent volumes valid",
			app: func() AppConfig {
				app := validReleaseApp("app")
				app.PersistentVolumes = []PersistentVolumeConfig{{ComposeName: "app-data", Name: "warpgate-app-data"}}
				return app
			}(),
			wantErr: false,
		},
		{
			name: "persistent volume missing compose name",
			app: func() AppConfig {
				app := validReleaseApp("app")
				app.PersistentVolumes = []PersistentVolumeConfig{{Name: "warpgate-app-data"}}
				return app
			}(),
			wantErr: true,
		},
		{
			name: "persistent volume missing name",
			app: func() AppConfig {
				app := validReleaseApp("app")
				app.PersistentVolumes = []PersistentVolumeConfig{{ComposeName: "app-data"}}
				return app
			}(),
			wantErr: true,
		},
		{
			name: "persistent volume invalid compose name",
			app: func() AppConfig {
				app := validReleaseApp("app")
				app.PersistentVolumes = []PersistentVolumeConfig{{ComposeName: "app data", Name: "warpgate-app-data"}}
				return app
			}(),
			wantErr: true,
		},
		{
			name: "persistent volume invalid name",
			app: func() AppConfig {
				app := validReleaseApp("app")
				app.PersistentVolumes = []PersistentVolumeConfig{{ComposeName: "app-data", Name: "warpgate/app/data"}}
				return app
			}(),
			wantErr: true,
		},
		{
			name: "persistent volume duplicate compose name",
			app: func() AppConfig {
				app := validReleaseApp("app")
				app.PersistentVolumes = []PersistentVolumeConfig{
					{ComposeName: "app-data", Name: "warpgate-app-data-a"},
					{ComposeName: "app-data", Name: "warpgate-app-data-b"},
				}
				return app
			}(),
			wantErr: true,
		},
		{
			name: "persistent volume duplicate name",
			app: func() AppConfig {
				app := validReleaseApp("app")
				app.PersistentVolumes = []PersistentVolumeConfig{
					{ComposeName: "app-data-a", Name: "warpgate-app-data"},
					{ComposeName: "app-data-b", Name: "warpgate-app-data"},
				}
				return app
			}(),
			wantErr: true,
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

	yml := `compose_ref: v1.0.0
targets: [node-1]
release:
  services:
    myapp:
      image: ghcr.io/org/myapp
      image_tag: v1.0.0
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

	if app.ComposeRef != "v1.0.0" {
		t.Errorf("expected compose_ref v1.0.0, got %s", app.ComposeRef)
	}
	if len(app.Targets) != 1 || app.Targets[0] != "node-1" {
		t.Errorf("unexpected targets: %v", app.Targets)
	}
	service := app.Release.Services["myapp"]
	if service.Image != "ghcr.io/org/myapp" {
		t.Errorf("expected service image ghcr.io/org/myapp, got %s", service.Image)
	}
	if service.EffectiveImageTag() != "v1.0.0" {
		t.Errorf("expected service image tag v1.0.0, got %s", service.EffectiveImageTag())
	}
	expose := service.EffectiveExpose()
	if expose.Public == nil || len(expose.Public.Domains) != 1 || expose.Public.Domains[0] != "myapp.example.com" {
		t.Errorf("unexpected public expose: %+v", expose.Public)
	}
	if expose.Private == nil || expose.Private.Port != 8080 {
		t.Errorf("unexpected private expose: %+v", expose.Private)
	}
	if expose.Internal == nil || expose.Internal.Hostname != "myapp.internal" {
		t.Errorf("unexpected internal expose: %+v", expose.Internal)
	}
	if service.SecretsPrefix != "myapp/prod" {
		t.Errorf("expected secrets_prefix myapp/prod, got %s", service.SecretsPrefix)
	}
	if service.Port != 8080 {
		t.Errorf("expected port 8080, got %d", service.Port)
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
		yml := "release:\n  services:\n    " + name + ":\n      image: ghcr.io/org/" + name + "\n"
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

	yml := "kind: warpgate/app\nrelease:\n  services:\n    myapp:\n      image: ghcr.io/org/myapp\n"
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

func appYAML(prefix, name string) string {
	return prefix + "release:\n  services:\n    " + name + ":\n      image: ghcr.io/org/" + name + "\n"
}

func TestDiscoverAppsKindFiltering(t *testing.T) {
	type appFixture struct {
		name string
		yml  string
	}

	tests := []struct {
		name      string
		apps      []appFixture
		wantNames []string
		wantKinds []string
	}{
		{
			name: "correct kind accepted",
			apps: []appFixture{
				{"myapp", appYAML("kind: warpgate/app\n", "myapp")},
			},
			wantNames: []string{"myapp"},
			wantKinds: []string{"warpgate/app"},
		},
		{
			name: "no kind accepted",
			apps: []appFixture{
				{"implicit-kind", appYAML("", "implicit-kind")},
			},
			wantNames: []string{"implicit-kind"},
			wantKinds: []string{""},
		},
		{
			name: "explicit empty kind accepted",
			apps: []appFixture{
				{"empty-kind", appYAML("kind: \"\"\n", "empty-kind")},
			},
			wantNames: []string{"empty-kind"},
			wantKinds: []string{""},
		},
		{
			name: "foreign kind skipped",
			apps: []appFixture{
				{"helm-app", appYAML("kind: helm/chart\n", "helm-app")},
			},
			wantNames: []string{},
			wantKinds: []string{},
		},
		{
			name: "case sensitive comparison",
			apps: []appFixture{
				{"upper", appYAML("kind: WARPGATE/APP\n", "upper")},
			},
			wantNames: []string{},
			wantKinds: []string{},
		},
		{
			name: "trailing whitespace rejected",
			apps: []appFixture{
				{"ws-app", appYAML("kind: \"warpgate/app \"\n", "ws-app")},
			},
			wantNames: []string{},
			wantKinds: []string{},
		},
		{
			name: "mixed: keeps valid and implicit kind, skips foreign",
			apps: []appFixture{
				{"aaa-valid", appYAML("kind: warpgate/app\n", "aaa-valid")},
				{"bbb-implicit", appYAML("", "bbb-implicit")},
				{"ccc-foreign", appYAML("kind: other/thing\n", "ccc-foreign")},
			},
			wantNames: []string{"aaa-valid", "bbb-implicit"},
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

func TestLoadAppConfigWithSource(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		wantSource      bool
		wantRepo        string
		wantComposePath string
		wantEnvironment map[string]string
		wantVolumes     []PersistentVolumeConfig
	}{
		{
			name: "full source and environment",
			yaml: `compose_ref: main
source:
  repo: github.com/pangobit/myapp
  compose_path: deploy/compose.yml
persistent_volumes:
  - compose_name: myapp-data
    name: warpgate-myapp-data
release:
  services:
    myapp:
      image: ghcr.io/org/myapp
      environment:
        DOMAIN: example.com
        AUTH_HOST: id.example.com
`,
			wantSource:      true,
			wantRepo:        "github.com/pangobit/myapp",
			wantComposePath: "deploy/compose.yml",
			wantEnvironment: map[string]string{
				"DOMAIN":    "example.com",
				"AUTH_HOST": "id.example.com",
			},
			wantVolumes: []PersistentVolumeConfig{
				{ComposeName: "myapp-data", Name: "warpgate-myapp-data"},
			},
		},
		{
			name: "minimal source",
			yaml: `compose_ref: main
source:
  repo: github.com/pangobit/myapp
release:
  services:
    myapp:
      image: ghcr.io/org/myapp
`,
			wantSource:      true,
			wantRepo:        "github.com/pangobit/myapp",
			wantComposePath: "",
		},
		{
			name: "source with environment and multiple persistent volumes",
			yaml: `compose_ref: main
source:
  repo: pangobit/app
persistent_volumes:
  - compose_name: data
    name: warpgate-myapp-data
  - compose_name: cache
    name: warpgate-myapp-cache
release:
  services:
    myapp:
      image: ghcr.io/org/myapp
      environment:
        KEY: val
`,
			wantSource: true,
			wantRepo:   "pangobit/app",
			wantEnvironment: map[string]string{
				"KEY": "val",
			},
			wantVolumes: []PersistentVolumeConfig{
				{ComposeName: "data", Name: "warpgate-myapp-data"},
				{ComposeName: "cache", Name: "warpgate-myapp-cache"},
			},
		},
		{
			name: "no source is valid",
			yaml: `release:
  services:
    myapp:
      image: ghcr.io/org/myapp
      image_tag: v1.0.0
`,
			wantSource: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			appDir := filepath.Join(dir, "myapp")
			if err := os.MkdirAll(appDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(appDir, "app.yml"), []byte(tt.yaml), 0644); err != nil {
				t.Fatal(err)
			}

			app, err := LoadAppConfig(appDir)
			if err != nil {
				t.Fatalf("LoadAppConfig() error: %v", err)
			}

			if !tt.wantSource {
				if app.Source != nil {
					t.Errorf("expected no Source, got repo=%q", app.Source.Repo)
				}
				return
			}

			if app.Source == nil {
				t.Fatalf("expected Source, got nil")
			}
			if app.Source.Repo != tt.wantRepo {
				t.Errorf("Source.Repo = %q, want %q", app.Source.Repo, tt.wantRepo)
			}
			if app.Source.ComposePath != tt.wantComposePath {
				t.Errorf("Source.ComposePath = %q, want %q", app.Source.ComposePath, tt.wantComposePath)
			}
			service := app.Release.Services["myapp"]
			if len(service.Environment) != len(tt.wantEnvironment) {
				t.Errorf("len(service.Environment) = %d, want %d", len(service.Environment), len(tt.wantEnvironment))
			}
			if len(app.PersistentVolumes) != len(tt.wantVolumes) {
				t.Errorf("len(PersistentVolumes) = %d, want %d", len(app.PersistentVolumes), len(tt.wantVolumes))
			}
			for key, want := range tt.wantEnvironment {
				if service.Environment[key] != want {
					t.Errorf("service.Environment[%q] = %q, want %q", key, service.Environment[key], want)
				}
			}
			for i, want := range tt.wantVolumes {
				if app.PersistentVolumes[i] != want {
					t.Errorf("PersistentVolumes[%d] = %+v, want %+v", i, app.PersistentVolumes[i], want)
				}
			}
		})
	}
}
