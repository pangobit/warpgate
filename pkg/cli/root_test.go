package cli

import (
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestDeployAllRejectsPositionalArgs(t *testing.T) {
	repo = &config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}
	defer func() { repo = nil }()

	deployAll = true
	defer func() { deployAll = false }()

	err := deployCmd.RunE(deployCmd, []string{"myapp"})
	if err == nil {
		t.Fatal("expected error when --all used with positional args")
	}
	if err.Error() != "--all does not accept app-name arguments" {
		t.Errorf("error = %q, want %q", err.Error(), "--all does not accept app-name arguments")
	}
}

func TestDeployRequiresAppNameOrAll(t *testing.T) {
	repo = &config.RepoConfig{
		Cluster: &config.ClusterConfig{Project: "test"},
	}
	defer func() { repo = nil }()

	deployAll = false

	err := deployCmd.RunE(deployCmd, []string{})
	if err == nil {
		t.Fatal("expected error when no app name and no --all")
	}
	if err.Error() != "requires app-name argument or --all flag" {
		t.Errorf("error = %q, want %q", err.Error(), "requires app-name argument or --all flag")
	}
}

func TestRemoveAllRequiresForce(t *testing.T) {
	removeAll = true
	removeForce = false
	defer func() {
		removeAll = false
		removeForce = false
	}()

	err := removeCmd.RunE(removeCmd, []string{})
	if err == nil {
		t.Fatal("expected error when --all used without --force")
	}
	if err.Error() != "remove --all requires --force flag" {
		t.Errorf("error = %q, want %q", err.Error(), "remove --all requires --force flag")
	}
}

func TestRemoveAllRejectsPositionalArgs(t *testing.T) {
	removeAll = true
	removeForce = true
	defer func() {
		removeAll = false
		removeForce = false
	}()

	err := removeCmd.RunE(removeCmd, []string{"myapp"})
	if err == nil {
		t.Fatal("expected error when --all used with positional args")
	}
	if err.Error() != "--all does not accept app-name arguments" {
		t.Errorf("error = %q, want %q", err.Error(), "--all does not accept app-name arguments")
	}
}

func TestRemoveRequiresAppNameOrAll(t *testing.T) {
	removeAll = false
	defer func() { removeAll = false }()

	err := removeCmd.RunE(removeCmd, []string{})
	if err == nil {
		t.Fatal("expected error when no app name and no --all")
	}
	if err.Error() != "requires app-name argument or --all flag" {
		t.Errorf("error = %q, want %q", err.Error(), "requires app-name argument or --all flag")
	}
}
