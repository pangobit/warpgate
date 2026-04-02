package cleanup

import (
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestBuildStepsBaseCount(t *testing.T) {
	cl := &Cleaner{
		Config: &config.ClusterConfig{},
	}

	steps := cl.buildSteps(nil)

	if len(steps) != 7 {
		t.Errorf("expected 7 base cleanup steps, got %d", len(steps))
	}

	expected := []string{
		"Detecting operating system",
		"Stopping app compose stacks",
		"Stopping Traefik",
		"Stopping SecretSauce server",
		"Removing Warpgate directories",
		"Removing warpgate Docker network",
		"Removing warpgate user",
	}

	for i, name := range expected {
		if steps[i].Name != name {
			t.Errorf("step %d: expected %q, got %q", i, name, steps[i].Name)
		}
	}
}

func TestBuildStepsWithRemoveGo(t *testing.T) {
	cl := &Cleaner{
		Config:   &config.ClusterConfig{},
		RemoveGo: true,
	}

	steps := cl.buildSteps(nil)

	if len(steps) != 8 {
		t.Errorf("expected 8 steps with --remove-go, got %d", len(steps))
	}
	if steps[7].Name != "Removing Go installation" {
		t.Errorf("expected 'Removing Go installation', got %q", steps[7].Name)
	}
}

func TestBuildStepsWithRemoveDocker(t *testing.T) {
	cl := &Cleaner{
		Config:       &config.ClusterConfig{},
		RemoveDocker: true,
	}

	steps := cl.buildSteps(nil)

	if len(steps) != 8 {
		t.Errorf("expected 8 steps with --remove-docker, got %d", len(steps))
	}
	if steps[7].Name != "Removing Docker" {
		t.Errorf("expected 'Removing Docker', got %q", steps[7].Name)
	}
}

func TestBuildStepsWithBothOptional(t *testing.T) {
	cl := &Cleaner{
		Config:       &config.ClusterConfig{},
		RemoveGo:     true,
		RemoveDocker: true,
	}

	steps := cl.buildSteps(nil)

	if len(steps) != 9 {
		t.Errorf("expected 9 steps with both optional flags, got %d", len(steps))
	}
}
