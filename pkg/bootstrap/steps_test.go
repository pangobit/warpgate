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

	if len(steps) != 8 {
		t.Errorf("expected 8 bootstrap steps, got %d", len(steps))
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
	}

	for i, name := range expected {
		if steps[i].Name != name {
			t.Errorf("step %d: expected %q, got %q", i, name, steps[i].Name)
		}
	}
}

func TestBuildStepsWithSecretsServer(t *testing.T) {
	cfg := &StepConfig{
		GoProxy:       "http://proxy:3000",
		Networking:    &config.NetworkingConfig{},
		SecretsServer: true,
	}

	steps := BuildSteps(nil, cfg)

	if len(steps) != 9 {
		t.Errorf("expected 9 steps with --secrets-server, got %d", len(steps))
	}

	if steps[8].Name != "Setting up SecretSauce server" {
		t.Errorf("expected last step to be 'Setting up SecretSauce server', got %q", steps[8].Name)
	}
}

func TestBuildStepsWithoutSecretsServer(t *testing.T) {
	cfg := &StepConfig{
		Networking: &config.NetworkingConfig{},
	}

	steps := BuildSteps(nil, cfg)

	if len(steps) != 8 {
		t.Errorf("expected 8 steps without --secrets-server, got %d", len(steps))
	}

	for _, step := range steps {
		if step.Name == "Setting up SecretSauce server" {
			t.Error("should not include SecretSauce server step without flag")
		}
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
