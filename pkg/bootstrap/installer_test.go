package bootstrap

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestWarpgateEnvScriptWithProxy(t *testing.T) {
	got := warpgateEnvScript("http://proxy.internal:3000")
	want := "" +
		"export GOPATH=/home/warpgate/go\n" +
		"export PATH=/usr/local/go/bin:$GOPATH/bin:$PATH\n" +
		"export GOPROXY='http://proxy.internal:3000'\n" +
		"export GONOSUMDB='github.com/pangobit/*'\n"

	if got != want {
		t.Fatalf("warpgateEnvScript() = %q, want %q", got, want)
	}
}

func TestWarpgateEnvScriptWithoutProxy(t *testing.T) {
	got := warpgateEnvScript("")
	want := "" +
		"export GOPATH=/home/warpgate/go\n" +
		"export PATH=/usr/local/go/bin:$GOPATH/bin:$PATH\n" +
		"export GONOSUMDB='github.com/pangobit/*'\n"

	if got != want {
		t.Fatalf("warpgateEnvScript() = %q, want %q", got, want)
	}
}

func TestShellSingleQuote(t *testing.T) {
	got := shellSingleQuote("a'b")
	if got != "a'\\''b" {
		t.Fatalf("shellSingleQuote() = %q, want %q", got, "a'\\''b")
	}
}

func TestSecretSauceInitCommandReadsPasswordFromStdin(t *testing.T) {
	got := secretSauceInitCommand()

	if got == "" {
		t.Fatal("secretSauceInitCommand() returned empty command")
	}
	if strings.Contains(got, "cat /tmp/.ss-init-pwd") {
		t.Fatalf("secretSauceInitCommand() = %q, must not read a temp password file", got)
	}
	if !strings.Contains(got, "IFS= read -r SS_MASTER_PASSWORD") {
		t.Fatalf("secretSauceInitCommand() = %q, want stdin read", got)
	}
	if !strings.Contains(got, `--password "$SS_MASTER_PASSWORD"`) {
		t.Fatalf("secretSauceInitCommand() = %q, want password expansion", got)
	}
}

func TestACMEChallengeModeDefaultsToTLS(t *testing.T) {
	if got := acmeChallengeMode(&config.NetworkingConfig{}); got != "tls" {
		t.Fatalf("acmeChallengeMode() = %q, want %q", got, "tls")
	}
}

func TestACMEChallengeModeReturnsLowercaseChallenge(t *testing.T) {
	got := acmeChallengeMode(&config.NetworkingConfig{
		Traefik: config.TraefikConfig{
			ACME: config.ACMEConfig{Challenge: "DNS"},
		},
	})
	if got != "dns" {
		t.Fatalf("acmeChallengeMode() = %q, want %q", got, "dns")
	}
}
