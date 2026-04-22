package deploy

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestDeployAllNoApps(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	err := d.DeployAll()
	if err == nil {
		t.Fatal("expected error for empty app list")
	}
	if err.Error() != "no apps found" {
		t.Errorf("error = %q, want %q", err.Error(), "no apps found")
	}
}

func TestRemoveAllNoApps(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	err := d.RemoveAll(nil)
	if err == nil {
		t.Fatal("expected error for empty app list")
	}
	if err.Error() != "no apps found" {
		t.Errorf("error = %q, want %q", err.Error(), "no apps found")
	}
}

func TestNeedsEnvFile(t *testing.T) {
	tests := []struct {
		name          string
		secretsPrefix string
		secretsServer string
		environment   map[string]string
		want          bool
	}{
		{
			name:          "secrets only",
			secretsPrefix: "app/",
			secretsServer: "https://secrets.example.com",
			want:          true,
		},
		{
			name:        "environment only",
			environment: map[string]string{"KEY": "val"},
			want:        true,
		},
		{
			name:          "both secrets and environment",
			secretsPrefix: "app/",
			secretsServer: "https://secrets.example.com",
			environment:   map[string]string{"KEY": "val"},
			want:          true,
		},
		{
			name: "neither",
			want: false,
		},
		{
			name:        "empty environment map",
			environment: map[string]string{},
			want:        false,
		},
		{
			name:          "secrets prefix without server",
			secretsPrefix: "app/",
			want:          false,
		},
		{
			name:          "secrets server without prefix",
			secretsServer: "https://secrets.example.com",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &config.AppConfig{
				SecretsPrefix: tt.secretsPrefix,
				Environment:   tt.environment,
			}
			got := needsEnvFile(app, tt.secretsServer)
			if got != tt.want {
				t.Errorf("needsEnvFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComposeUpCommand(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")

	tests := []struct {
		name       string
		hasEnvFile bool
		wantEnvArg bool
	}{
		{"with env file", true, true},
		{"without env file", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.composeUpCommand(
				&config.AppConfig{},
				"-p myapp-blue",
				"-f compose.yml -f docker-compose.override.yml",
				tt.hasEnvFile,
			)
			hasArg := strings.Contains(got, "--env-file .env")
			if hasArg != tt.wantEnvArg {
				t.Errorf("composeUpCommand() = %q, wantEnvArg %v", got, tt.wantEnvArg)
			}
			if !strings.HasSuffix(got, "up -d") {
				t.Errorf("composeUpCommand() = %q, want suffix 'up -d'", got)
			}
		})
	}
}

func TestMakeDeployPlan(t *testing.T) {
	tests := []struct {
		name     string
		strategy config.DeployStrategy
		state    *DeployState
		want     deployPlan
	}{
		{
			name:     "blue green initial deploy starts on green",
			strategy: config.StrategyBlueGreen,
			state:    &DeployState{},
			want: deployPlan{
				activeSlot:  "green",
				projectFlag: "-p myapp-green",
			},
		},
		{
			name:     "blue green flips from blue to green",
			strategy: config.StrategyBlueGreen,
			state: &DeployState{
				ActiveSlot: "blue",
			},
			want: deployPlan{
				activeSlot:  "green",
				prevSlot:    "blue",
				projectFlag: "-p myapp-green",
			},
		},
		{
			name:     "blue green flips from green to blue",
			strategy: config.StrategyBlueGreen,
			state: &DeployState{
				ActiveSlot: "green",
			},
			want: deployPlan{
				activeSlot:  "blue",
				prevSlot:    "green",
				projectFlag: "-p myapp-blue",
			},
		},
		{
			name:     "recreate ignores stale blue green slot",
			strategy: config.StrategyRecreate,
			state: &DeployState{
				ActiveSlot: "green",
			},
			want: deployPlan{
				projectFlag: "-p myapp",
			},
		},
		{
			name:     "nil state is treated as empty state",
			strategy: config.StrategyBlueGreen,
			state:    nil,
			want: deployPlan{
				activeSlot:  "green",
				projectFlag: "-p myapp-green",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeDeployPlan("myapp", tt.strategy, tt.state)
			if got != tt.want {
				t.Errorf("makeDeployPlan() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestActiveProjectFlag(t *testing.T) {
	tests := []struct {
		name       string
		strategy   config.DeployStrategy
		activeSlot string
		wantFlag   string
		wantOK     bool
	}{
		{
			name:       "blue green with active slot returns project",
			strategy:   config.StrategyBlueGreen,
			activeSlot: "blue",
			wantFlag:   "-p probe-blue",
			wantOK:     true,
		},
		{
			name:     "blue green without active slot is not deployed",
			strategy: config.StrategyBlueGreen,
			wantOK:   false,
		},
		{
			name:       "recreate with empty slot still resolves active project",
			strategy:   config.StrategyRecreate,
			activeSlot: "",
			wantFlag:   "-p probe",
			wantOK:     true,
		},
		{
			name:       "recreate ignores adversarial stale slot",
			strategy:   config.StrategyRecreate,
			activeSlot: "green",
			wantFlag:   "-p probe",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFlag, gotOK := activeProjectFlag("probe", tt.strategy, tt.activeSlot)
			if gotOK != tt.wantOK {
				t.Fatalf("activeProjectFlag() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotFlag != tt.wantFlag {
				t.Errorf("activeProjectFlag() flag = %q, want %q", gotFlag, tt.wantFlag)
			}
		})
	}
}

func TestAllDeploymentProjectFlags(t *testing.T) {
	got := allDeploymentProjectFlags("probe")
	want := []string{
		"-p probe",
		"-p probe-blue",
		"-p probe-green",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("allDeploymentProjectFlags() = %v, want %v", got, want)
	}
}

func TestFetchSecretsEnvironmentOnly(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")

	app := &config.AppConfig{
		Environment: map[string]string{
			"DOMAIN":    "example.com",
			"LOG_LEVEL": "info",
		},
	}

	got, err := d.fetchSecrets(app)
	if err != nil {
		t.Fatalf("fetchSecrets() error = %v", err)
	}
	if got == "" {
		t.Fatal("fetchSecrets() returned empty string, want .env content")
	}
	if !strings.Contains(got, "DOMAIN=example.com") {
		t.Errorf("fetchSecrets() missing DOMAIN, got:\n%s", got)
	}
	if !strings.Contains(got, "LOG_LEVEL=info") {
		t.Errorf("fetchSecrets() missing LOG_LEVEL, got:\n%s", got)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", "''"},
		{"simple alphanumeric", "myapp", "'myapp'"},
		{"with hyphen", "my-app", "'my-app'"},
		{"single quote", "it's", "'it'\\''s'"},
		{"multiple single quotes", "a'b'c", "'a'\\''b'\\''c'"},
		{"backtick injection", "app`whoami`", "'app`whoami`'"},
		{"dollar expansion", "app$HOME", "'app$HOME'"},
		{"semicolon injection", "app;rm -rf /", "'app;rm -rf /'"},
		{"pipe injection", "app|cat /etc/passwd", "'app|cat /etc/passwd'"},
		{"subshell injection", "$(id)", "'$(id)'"},
		{"double quotes", `app"name`, `'app"name'`},
		{"newline", "line1\nline2", "'line1\nline2'"},
		{"ampersand", "a&&b", "'a&&b'"},
		{"space", "hello world", "'hello world'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
