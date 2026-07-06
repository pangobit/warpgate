// Package version reports the running Warpgate binary version.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Version is the release version injected at build time.
var Version = "dev"

// Current returns the running binary version.
func Current() string {
	if v := normalize(Version); v != "" && v != "dev" {
		return v
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := normalize(info.Main.Version); v != "" && v != "(devel)" {
		return v
	}
	return "dev"
}

// Platform returns the operating system and architecture of the running binary.
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func normalize(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if version == "dev" || version == "(devel)" {
		return version
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}
