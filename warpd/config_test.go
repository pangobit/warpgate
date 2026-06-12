package warpd

import "testing"

func TestDefaultServeConfigReadsRepositoryFromEnv(t *testing.T) {
	t.Setenv("WARPGATE_REPO", "pangobit/deploy")
	t.Setenv("WARPGATE_REPO_BRANCH", "")
	t.Setenv("WARPGATE_HTTP_ADDR", "")
	t.Setenv("WARPGATE_SSH_ADDR", "")

	cfg := DefaultServeConfig()

	if cfg.Repository.Owner != "pangobit" || cfg.Repository.Repo != "deploy" {
		t.Fatalf("repository = %+v, want pangobit/deploy", cfg.Repository)
	}
	if cfg.Repository.Branch != "master" {
		t.Fatalf("branch = %q, want master", cfg.Repository.Branch)
	}
	if !cfg.Deploy.TailscaleSSH {
		t.Fatal("TailscaleSSH should default to true")
	}
	if cfg.HTTPAddr == "" || cfg.SSHAddr == "" {
		t.Fatalf("listen addresses must have defaults, got %q / %q", cfg.HTTPAddr, cfg.SSHAddr)
	}
}

func TestDefaultServeConfigIgnoresMalformedRepo(t *testing.T) {
	t.Setenv("WARPGATE_REPO", "not-a-repo")

	cfg := DefaultServeConfig()

	if cfg.Repository.Owner != "" || cfg.Repository.Repo != "" {
		t.Fatalf("repository = %+v, want empty for malformed WARPGATE_REPO", cfg.Repository)
	}
}
