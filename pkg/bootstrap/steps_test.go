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

	if len(steps) != 10 {
		t.Errorf("expected 10 bootstrap steps, got %d", len(steps))
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

	if len(steps) != 10 {
		t.Errorf("expected 10 bootstrap steps, got %d", len(steps))
	}

	if steps[9].Name != "Setting up SecretSauce server" {
		t.Errorf("expected last step to be 'Setting up SecretSauce server', got %q", steps[9].Name)
	}
}

func TestBuildStepsWithoutGoProxyStillIncludeSecretsServer(t *testing.T) {
	cfg := &StepConfig{
		Networking: &config.NetworkingConfig{},
	}

	steps := BuildSteps(nil, cfg)

	if len(steps) != 10 {
		t.Errorf("expected 10 bootstrap steps, got %d", len(steps))
	}

	if steps[9].Name != "Setting up SecretSauce server" {
		t.Errorf("expected last step to be 'Setting up SecretSauce server', got %q", steps[9].Name)
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
