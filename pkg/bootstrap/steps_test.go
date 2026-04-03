package bootstrap

import (
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestBuildStepsCount(t *testing.T) {
	cfg := &StepConfig{
		GoProxy:    "http://proxy:3000",
		Networking: &config.NetworkingConfig{},
	}

	steps := BuildSteps(nil, cfg)

	if len(steps) != 11 {
		t.Errorf("expected 11 bootstrap steps, got %d", len(steps))
	}

	expected := []string{
		"Detecting operating system",
		"Creating warpgate user",
		"Installing Go",
		"Installing Docker",
		"Configuring docker group",
		"Installing SecretSauce",
		"Setting up SSH keys",
		"Setting up Warpgate + Traefik",
		"Setting up Internal Proxy",
		"Setting up SecretSauce server",
		"Storing registry credentials",
	}

	for i, name := range expected {
		if steps[i].Name != name {
			t.Errorf("step %d: expected %q, got %q", i, name, steps[i].Name)
		}
	}
}

func TestBuildStepsAlwaysIncludeSecretsServer(t *testing.T) {
	cfg := &StepConfig{
		GoProxy:    "http://proxy:3000",
		Networking: &config.NetworkingConfig{},
	}

	steps := BuildSteps(nil, cfg)

	if len(steps) != 11 {
		t.Errorf("expected 11 bootstrap steps, got %d", len(steps))
	}

	if steps[10].Name != "Storing registry credentials" {
		t.Errorf("expected last step to be 'Storing registry credentials', got %q", steps[10].Name)
	}
}

func TestBuildStepsWithoutGoProxyStillIncludeSecretsServer(t *testing.T) {
	cfg := &StepConfig{
		Networking: &config.NetworkingConfig{},
	}

	steps := BuildSteps(nil, cfg)

	if len(steps) != 11 {
		t.Errorf("expected 11 bootstrap steps, got %d", len(steps))
	}

	if steps[10].Name != "Storing registry credentials" {
		t.Errorf("expected last step to be 'Storing registry credentials', got %q", steps[10].Name)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"has space", "'has space'"},
		{"it's", "'it'\\''s'"},
		{"ghp_abc123", "'ghp_abc123'"},
		{"", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStoreRegistryCredentialsSkipsWhenEmpty(t *testing.T) {
	tests := []struct {
		name string
		reg  *config.RegistryConfig
	}{
		{"nil", nil},
		{"empty", &config.RegistryConfig{}},
		{"no_password", &config.RegistryConfig{Server: "ghcr.io", Username: "user"}},
		{"no_username", &config.RegistryConfig{Server: "ghcr.io", Password: "pass"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := storeRegistryCredentials(nil, tt.reg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msg != "skipped — no credentials provided" {
				t.Errorf("expected skip message, got %q", msg)
			}
		})
	}
}

func TestGoArch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"amd64", "amd64"},
		{"x86_64", "amd64"},
		{"arm64", "arm64"},
		{"aarch64", "arm64"},
		{"arm", "armv6l"},
		{"386", "386"},
		{"unknown", "amd64"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			osInfo := &OSInfo{Arch: tt.input}
			got := osInfo.goArch()
			if got != tt.want {
				t.Errorf("goArch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
