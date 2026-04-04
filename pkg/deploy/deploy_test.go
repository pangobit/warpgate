package deploy

import (
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
