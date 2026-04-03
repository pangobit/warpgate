package bootstrap

import "testing"

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
