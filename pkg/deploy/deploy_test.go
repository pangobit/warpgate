package deploy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/release"
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

func TestReleaseEnvFileMap(t *testing.T) {
	tests := []struct {
		name          string
		manifest      *release.Manifest
		secretsServer string
		want          map[string]bool
	}{
		{
			name: "selects service with fetchable secrets",
			manifest: &release.Manifest{Services: map[string]release.ServiceManifest{
				"api": {SecretsPrefix: "app/"},
			}},
			secretsServer: "https://secrets.example.com",
			want:          map[string]bool{"api": true},
		},
		{
			name: "selects service with environment",
			manifest: &release.Manifest{Services: map[string]release.ServiceManifest{
				"api": {Environment: map[string]string{"KEY": "val"}},
			}},
			want: map[string]bool{"api": true},
		},
		{
			name: "does not select secrets without server",
			manifest: &release.Manifest{Services: map[string]release.ServiceManifest{
				"api": {SecretsPrefix: "app/"},
			}},
			want: map[string]bool{},
		},
		{
			name: "selects only services with env inputs",
			manifest: &release.Manifest{Services: map[string]release.ServiceManifest{
				"api":    {Environment: map[string]string{"KEY": "val"}},
				"admin":  {SecretsPrefix: "admin/"},
				"worker": {},
			}},
			secretsServer: "https://secrets.example.com",
			want:          map[string]bool{"api": true, "admin": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseEnvFileMap(tt.manifest, tt.secretsServer)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("releaseEnvFileMap() = %v, want %v", got, tt.want)
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

func TestPrepareRecreateRestoreLoadsPreviousReleaseAndFiles(t *testing.T) {
	root := t.TempDir()
	previousManifest := &release.Manifest{
		ID:  "old-release",
		App: "probe",
		Services: map[string]release.ServiceManifest{
			"probe": {
				ImageRef: "ghcr.io/example/probe:v1",
			},
		},
	}
	if err := release.Save(filepath.Join(root, "apps", "probe", "releases"), previousManifest); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	d := NewDeployer(&config.RepoConfig{
		Root:    root,
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	runner := &fakeRemote{
		commandStdout: map[string]string{
			readRemoteTextFileCommand("/remote/compose.yml"):                 "1\nold compose",
			readRemoteTextFileCommand("/remote/docker-compose.override.yml"): "1\nold override",
		},
	}

	got, err := d.prepareRecreateRestore(runner, "/remote", &config.AppConfig{Name: "probe"}, &DeployState{CurrentRelease: "old-release"})
	if err != nil {
		t.Fatalf("prepareRecreateRestore() error = %v", err)
	}
	if !got.canRestore() {
		t.Fatal("prepareRecreateRestore() cannot restore, want true")
	}
	if got.files.compose != "old compose" || got.files.override != "old override" {
		t.Fatalf("restore files = %+v", got.files)
	}
	if got.manifest == nil || got.manifest.ID != "old-release" {
		t.Fatalf("restore manifest = %+v, want old-release", got.manifest)
	}

	wantCommands := []string{
		readRemoteTextFileCommand("/remote/compose.yml"),
		readRemoteTextFileCommand("/remote/docker-compose.override.yml"),
	}
	if !reflect.DeepEqual(runner.commands, wantCommands) {
		t.Errorf("commands = %v, want %v", runner.commands, wantCommands)
	}
}

func TestPrepareRecreateRestoreFailsWhenRecordedDeploymentFilesAreMissing(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	runner := &fakeRemote{
		commandStdout: map[string]string{
			readRemoteTextFileCommand("/remote/compose.yml"):                 "0\n",
			readRemoteTextFileCommand("/remote/docker-compose.override.yml"): "0\n",
		},
	}

	_, err := d.prepareRecreateRestore(runner, "/remote", &config.AppConfig{Name: "probe"}, &DeployState{CurrentVersion: "v1"})
	if err == nil || err.Error() != "cannot prepare recreate restore for probe: previous compose files are missing" {
		t.Fatalf("prepareRecreateRestore() error = %v", err)
	}
}

func TestRestorePreviousRecreateDeploymentRestartsCapturedRelease(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	runner := &fakeRemote{}
	restorePlan := recreateRestorePlan{
		files: deploymentFileSnapshot{
			compose:     "old compose",
			override:    "old override",
			hasCompose:  true,
			hasOverride: true,
		},
		manifest: &release.Manifest{
			App: "probe",
			Services: map[string]release.ServiceManifest{
				"probe": {
					Environment: map[string]string{
						"DOMAIN": "old.example",
					},
				},
			},
		},
	}

	var waitFlags []string
	waitForHealth := func(_ commandRunner, _ string, _ string, projectFlag string, _ string, _ logger) error {
		waitFlags = append(waitFlags, projectFlag)
		return nil
	}

	err := d.restorePreviousRecreateDeployment(
		runner,
		"/remote",
		&config.AppConfig{Name: "probe", Strategy: config.StrategyRecreate},
		restorePlan,
		"-f compose.yml -f docker-compose.override.yml",
		false,
		waitForHealth,
	)
	if err != nil {
		t.Fatalf("restorePreviousRecreateDeployment() error = %v", err)
	}

	wantWrites := []string{
		"/remote/compose.yml=old compose",
		"/remote/docker-compose.override.yml=old override",
	}
	if !reflect.DeepEqual(runner.writes, wantWrites) {
		t.Errorf("writes = %v, want %v", runner.writes, wantWrites)
	}

	wantSecretWrites := []string{
		"/remote/.env.probe=DOMAIN=old.example\n",
		"/remote/.env=DOMAIN=old.example\n",
	}
	if !reflect.DeepEqual(runner.secretWrites, wantSecretWrites) {
		t.Errorf("secret writes = %v, want %v", runner.secretWrites, wantSecretWrites)
	}

	wantCommands := []string{
		"cd /remote && docker compose -p probe -f compose.yml -f docker-compose.override.yml --env-file .env up -d",
		"rm -f /remote/.env /remote/.env.probe",
	}
	if !reflect.DeepEqual(runner.commands, wantCommands) {
		t.Errorf("commands = %v, want %v", runner.commands, wantCommands)
	}

	if !reflect.DeepEqual(waitFlags, []string{"-p probe"}) {
		t.Errorf("wait flags = %v, want [-p probe]", waitFlags)
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

func TestWriteReleaseEnvFileWritesPerServiceAndProjectEnv(t *testing.T) {
	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}, "")
	manifest := &release.Manifest{
		App: "api",
		Services: map[string]release.ServiceManifest{
			"api": {
				Environment: map[string]string{
					"DOMAIN": "example.com",
				},
			},
			"admin": {
				Environment: map[string]string{
					"ADMIN_DOMAIN": "admin.example.com",
				},
			},
			"worker": {},
		},
	}

	writer := &fakeRemote{}
	got, err := d.writeReleaseEnvFile(writer, "/remote", manifest)
	if err != nil {
		t.Fatalf("writeReleaseEnvFile() error = %v", err)
	}
	if !got {
		t.Fatal("writeReleaseEnvFile() = false, want true")
	}

	want := []string{
		"/remote/.env.admin=ADMIN_DOMAIN=admin.example.com\n",
		"/remote/.env.api=DOMAIN=example.com\n",
		"/remote/.env=ADMIN_DOMAIN=admin.example.com\nDOMAIN=example.com\n",
	}
	if !reflect.DeepEqual(writer.secretWrites, want) {
		t.Errorf("secret writes = %v, want %v", writer.secretWrites, want)
	}
}

func TestWriteReleaseEnvFileWritesSharedSecretsPrefixToEachService(t *testing.T) {
	var listRequests int
	var valueRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/secrets" && r.URL.Query().Get("prefix") == "shared/prod":
			listRequests++
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, []map[string]string{
				{"key": "shared/prod/api_key", "state": "active"},
			})
		case r.URL.RawPath == "/api/secrets/shared%2Fprod%2Fapi_key" || r.URL.Path == "/api/secrets/shared/prod/api_key":
			valueRequests++
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]string{
				"key": "shared/prod/api_key", "value": "secret123", "state": "active",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	d := NewDeployer(&config.RepoConfig{
		Cluster: &config.ClusterConfig{
			Project: "test",
			Secrets: config.SecretsConfig{Server: server.URL},
		},
	}, "")
	manifest := &release.Manifest{
		App: "api",
		Services: map[string]release.ServiceManifest{
			"api": {
				SecretsPrefix: "shared/prod",
			},
			"worker": {
				SecretsPrefix: "shared/prod",
			},
		},
	}

	writer := &fakeRemote{}
	got, err := d.writeReleaseEnvFile(writer, "/remote", manifest)
	if err != nil {
		t.Fatalf("writeReleaseEnvFile() error = %v", err)
	}
	if !got {
		t.Fatal("writeReleaseEnvFile() = false, want true")
	}

	want := []string{
		"/remote/.env.api=API_KEY=secret123\n",
		"/remote/.env.worker=API_KEY=secret123\n",
		"/remote/.env=API_KEY=secret123\n",
	}
	if !reflect.DeepEqual(writer.secretWrites, want) {
		t.Errorf("secret writes = %v, want %v", writer.secretWrites, want)
	}
	if listRequests != 2 {
		t.Errorf("list requests = %d, want 2", listRequests)
	}
	if valueRequests != 2 {
		t.Errorf("value requests = %d, want 2", valueRequests)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("json encode response: %v", err)
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
