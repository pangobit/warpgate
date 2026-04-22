package deploy

import (
	"errors"
	"os"
	"path/filepath"
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

func TestStatusProjectFlags(t *testing.T) {
	tests := []struct {
		name       string
		activeSlot string
		want       []string
	}{
		{
			name: "recreate-first when no active slot",
			want: []string{
				"-p probe",
				"-p probe-blue",
				"-p probe-green",
			},
		},
		{
			name:       "active blue checked before fallbacks",
			activeSlot: "blue",
			want: []string{
				"-p probe-blue",
				"-p probe",
				"-p probe-green",
			},
		},
		{
			name:       "active green checked before fallbacks",
			activeSlot: "green",
			want: []string{
				"-p probe-green",
				"-p probe",
				"-p probe-blue",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusProjectFlags("probe", tt.activeSlot)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("statusProjectFlags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindProjectPS(t *testing.T) {
	composeFiles := "-f compose.yml -f docker-compose.override.yml"
	format := "{{.Name}}\t{{.Status}}"

	tests := []struct {
		name          string
		activeSlot    string
		commandStdout map[string]string
		wantFlag      string
		wantOutput    string
		wantFound     bool
		wantCommands  []string
	}{
		{
			name:       "recreate finds stable project first",
			activeSlot: "",
			commandStdout: map[string]string{
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'": "probe-app-1\tUp 1 minute",
			},
			wantFlag:   "-p probe",
			wantOutput: "probe-app-1\tUp 1 minute",
			wantFound:  true,
			wantCommands: []string{
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
			},
		},
		{
			name:       "falls back from stale active slot to stable project",
			activeSlot: "green",
			commandStdout: map[string]string{
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'": "NOT_DEPLOYED",
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'":       "probe-app-1\tUp 1 minute",
			},
			wantFlag:   "-p probe",
			wantOutput: "probe-app-1\tUp 1 minute",
			wantFound:  true,
			wantCommands: []string{
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
			},
		},
		{
			name:       "falls back to legacy blue project when state is empty",
			activeSlot: "",
			commandStdout: map[string]string{
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'":      "NOT_DEPLOYED",
				"cd /remote && docker compose -p probe-blue -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'": "probe-blue-app-1\tUp 1 minute",
			},
			wantFlag:   "-p probe-blue",
			wantOutput: "probe-blue-app-1\tUp 1 minute",
			wantFound:  true,
			wantCommands: []string{
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
				"cd /remote && docker compose -p probe-blue -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRemote{commandStdout: tt.commandStdout}
			gotFlag, gotOutput, gotFound := findProjectPS(runner, "/remote", "probe", tt.activeSlot, composeFiles, format)
			if gotFlag != tt.wantFlag || gotOutput != tt.wantOutput || gotFound != tt.wantFound {
				t.Fatalf("findProjectPS() = (%q, %q, %v), want (%q, %q, %v)", gotFlag, gotOutput, gotFound, tt.wantFlag, tt.wantOutput, tt.wantFound)
			}
			if !reflect.DeepEqual(runner.commands, tt.wantCommands) {
				t.Errorf("findProjectPS() commands = %v, want %v", runner.commands, tt.wantCommands)
			}
		})
	}
}

func TestDeployWithStrategy(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")

	tests := []struct {
		name          string
		app           *config.AppConfig
		state         *DeployState
		waitErr       error
		wantResult    deployResult
		wantErr       string
		wantCommands  []string
		wantWaitFlags []string
	}{
		{
			name: "recreate uses stable project and cleans legacy slots",
			app: &config.AppConfig{
				Name:     "probe",
				Strategy: config.StrategyRecreate,
			},
			state: &DeployState{
				ActiveSlot: "green",
			},
			wantResult: deployResult{},
			wantCommands: []string{
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml pull",
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml down",
				"cd /remote && docker compose -p probe-blue -f compose.yml -f docker-compose.override.yml down",
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml down",
				"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml up -d",
			},
			wantWaitFlags: []string{"-p probe"},
		},
		{
			name: "blue green promotes next slot and stops previous slot after health",
			app: &config.AppConfig{
				Name:     "probe",
				Strategy: config.StrategyBlueGreen,
			},
			state: &DeployState{
				ActiveSlot: "blue",
			},
			wantResult: deployResult{ActiveSlot: "green"},
			wantCommands: []string{
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml pull",
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml up -d",
				"cd /remote && docker compose -p probe-blue -f compose.yml -f docker-compose.override.yml down",
			},
			wantWaitFlags: []string{"-p probe-green"},
		},
		{
			name: "health failure tears down only the new project",
			app: &config.AppConfig{
				Name:     "probe",
				Strategy: config.StrategyBlueGreen,
			},
			state: &DeployState{
				ActiveSlot: "blue",
			},
			waitErr: errors.New("unhealthy"),
			wantErr: "health check failed: unhealthy",
			wantCommands: []string{
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml pull",
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml up -d",
				"cd /remote && docker compose -p probe-green -f compose.yml -f docker-compose.override.yml down",
			},
			wantWaitFlags: []string{"-p probe-green"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRemote{}
			var gotWaitFlags []string
			waitForHealth := func(_ commandRunner, _ string, _ string, projectFlag string, _ string, _ logger) error {
				gotWaitFlags = append(gotWaitFlags, projectFlag)
				return tt.waitErr
			}

			got, err := d.deployWithStrategy(runner, "/remote", tt.app, tt.state, "-f compose.yml -f docker-compose.override.yml", false, waitForHealth)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("deployWithStrategy() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("deployWithStrategy() error = %v", err)
			}

			if got != tt.wantResult {
				t.Errorf("deployWithStrategy() result = %+v, want %+v", got, tt.wantResult)
			}
			if !reflect.DeepEqual(runner.commands, tt.wantCommands) {
				t.Errorf("deployWithStrategy() commands = %v, want %v", runner.commands, tt.wantCommands)
			}
			if !reflect.DeepEqual(gotWaitFlags, tt.wantWaitFlags) {
				t.Errorf("deployWithStrategy() wait flags = %v, want %v", gotWaitFlags, tt.wantWaitFlags)
			}
		})
	}
}

func TestStartAndVerifyProjectStartFailure(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	runner := &fakeRemote{
		commandErrs: map[string]error{
			"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml up -d": errors.New("boom"),
		},
		commandStderr: map[string]string{
			"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml up -d": "bad start",
		},
	}
	waitCalled := false
	waitForHealth := func(_ commandRunner, _ string, _ string, _ string, _ string, _ logger) error {
		waitCalled = true
		return nil
	}

	err := d.startAndVerifyProject(runner, "/remote", &config.AppConfig{Name: "probe"}, "-p probe", "-f compose.yml -f docker-compose.override.yml", false, "probe", waitForHealth)
	if err == nil || err.Error() != "deploy failed: boom\nbad start" {
		t.Fatalf("startAndVerifyProject() error = %v", err)
	}
	if waitCalled {
		t.Fatal("startAndVerifyProject() called waitForHealth after start failure")
	}
}

func TestUploadExtraFiles(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	appDir := t.TempDir()
	files := map[string]string{
		"app.yml":     "skip",
		"compose.yml": "skip",
		"notes.txt":   "hello",
		"config.json": "{}",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(appDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	uploader := &fakeRemote{}
	if err := d.uploadExtraFiles(uploader, appDir, "/remote"); err != nil {
		t.Fatalf("uploadExtraFiles() error = %v", err)
	}

	wantUploads := []string{
		appDir + "/config.json->/remote/config.json",
		appDir + "/notes.txt->/remote/notes.txt",
	}
	if !reflect.DeepEqual(uploader.uploads, wantUploads) {
		t.Errorf("uploadExtraFiles() uploads = %v, want %v", uploader.uploads, wantUploads)
	}
}

func TestWriteEnvFile(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")

	tests := []struct {
		name       string
		app        *config.AppConfig
		wantWrote  bool
		wantSecret string
	}{
		{
			name: "environment writes env file",
			app: &config.AppConfig{
				Environment: map[string]string{"DOMAIN": "example.com"},
			},
			wantWrote:  true,
			wantSecret: "/remote/.env=DOMAIN=example.com\n",
		},
		{
			name: "no environment and no secrets skips env file",
			app:  &config.AppConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeRemote{}
			got, err := d.writeEnvFile(writer, "/remote", tt.app)
			if err != nil {
				t.Fatalf("writeEnvFile() error = %v", err)
			}
			if got != tt.wantWrote {
				t.Fatalf("writeEnvFile() = %v, want %v", got, tt.wantWrote)
			}
			if tt.wantSecret == "" {
				if len(writer.secretWrites) != 0 {
					t.Fatalf("writeEnvFile() wrote unexpected secret files: %v", writer.secretWrites)
				}
				return
			}
			if !reflect.DeepEqual(writer.secretWrites, []string{tt.wantSecret}) {
				t.Errorf("writeEnvFile() secret writes = %v, want %v", writer.secretWrites, []string{tt.wantSecret})
			}
		})
	}
}

func TestLoginRegistry(t *testing.T) {
	tests := []struct {
		name      string
		registry  config.RegistryConfig
		wantCalls []string
		wantStdin []string
	}{
		{
			name: "configured registry logs in",
			registry: config.RegistryConfig{
				Server:   "ghcr.io",
				Username: "alice",
				Password: "secret",
			},
			wantCalls: []string{
				"docker login 'ghcr.io' -u 'alice' --password-stdin",
			},
			wantStdin: []string{"secret"},
		},
		{
			name:     "missing username skips login",
			registry: config.RegistryConfig{Server: "ghcr.io"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDeployer(&config.RepoConfig{
				Cluster: &config.ClusterConfig{
					Project:  "test",
					Registry: tt.registry,
				},
			}, "")
			runner := &fakeRemote{}
			if err := d.loginRegistry(runner); err != nil {
				t.Fatalf("loginRegistry() error = %v", err)
			}
			if !reflect.DeepEqual(runner.stdinCommands, tt.wantCalls) {
				t.Errorf("loginRegistry() commands = %v, want %v", runner.stdinCommands, tt.wantCalls)
			}
			if !reflect.DeepEqual(runner.stdinPayloads, tt.wantStdin) {
				t.Errorf("loginRegistry() stdin = %v, want %v", runner.stdinPayloads, tt.wantStdin)
			}
		})
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

type fakeRemote struct {
	commands      []string
	commandStdout map[string]string
	commandStderr map[string]string
	commandErrs   map[string]error
	stdinCommands []string
	stdinPayloads []string
	writes        []string
	secretWrites  []string
	uploads       []string
}

func (f *fakeRemote) RunCommand(cmd string) (string, string, error) {
	f.commands = append(f.commands, cmd)
	return f.commandStdout[cmd], f.commandStderr[cmd], f.commandErrs[cmd]
}

func (f *fakeRemote) RunCommandStdin(cmd, stdinData string) (string, string, error) {
	f.stdinCommands = append(f.stdinCommands, cmd)
	f.stdinPayloads = append(f.stdinPayloads, stdinData)
	return "", "", nil
}

func (f *fakeRemote) WriteFile(remotePath, content string) error {
	f.writes = append(f.writes, remotePath+"="+content)
	return nil
}

func (f *fakeRemote) WriteFileSecret(remotePath, content string) error {
	f.secretWrites = append(f.secretWrites, remotePath+"="+content)
	return nil
}

func (f *fakeRemote) UploadFile(localPath, remotePath string) error {
	f.uploads = append(f.uploads, localPath+"->"+remotePath)
	return nil
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
