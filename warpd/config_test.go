package warpd

import "testing"

func TestDefaultLocalUIConfigUsesTailscaleSSH(t *testing.T) {
	cfg := DefaultLocalUIConfig()

	cfg.Deploy.User = "root"

	if !cfg.Deploy.TailscaleSSH {
		t.Fatalf("TailscaleSSH = false, want true")
	}
	if cfg.Deploy.User != "root" {
		t.Fatalf("user = %q, want root", cfg.Deploy.User)
	}
}
