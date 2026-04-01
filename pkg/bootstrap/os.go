package bootstrap

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// OSDistro represents the detected Linux distribution.
type OSDistro int

const (
	Unknown OSDistro = iota
	Ubuntu
	Debian
	CentOS
	Fedora
	RockyLinux
	AlmaLinux
	RHEL
	AmazonLinux
	Arch
)

// String returns the human-readable name of the distribution.
func (o OSDistro) String() string {
	switch o {
	case Ubuntu:
		return "Ubuntu"
	case Debian:
		return "Debian"
	case CentOS:
		return "CentOS"
	case Fedora:
		return "Fedora"
	case RockyLinux:
		return "Rocky Linux"
	case AlmaLinux:
		return "AlmaLinux"
	case RHEL:
		return "RHEL"
	case AmazonLinux:
		return "Amazon Linux"
	case Arch:
		return "Arch Linux"
	default:
		return "Unknown"
	}
}

// OSInfo holds detected OS information.
type OSInfo struct {
	// Distro is the detected Linux distribution.
	Distro OSDistro
	// Version is the distribution version string.
	Version string
	// Arch is the CPU architecture.
	Arch string
}

// DetectOS detects the operating system of the local machine.
func DetectOS() *OSInfo {
	info := &OSInfo{
		Arch: runtime.GOARCH,
	}

	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		info.Distro, info.Version = parseOSRelease(string(data))
	}

	if info.Distro == Unknown {
		if data, err := os.ReadFile("/etc/lsb-release"); err == nil {
			info.Distro, info.Version = parseLSBRelease(string(data))
		}
	}

	if info.Distro == Unknown {
		if _, err := os.Stat("/etc/redhat-release"); err == nil {
			if data, err := os.ReadFile("/etc/redhat-release"); err == nil {
				info.Distro, info.Version = parseRedHatRelease(string(data))
			}
		}
	}

	return info
}

// DetectOSFromOutput parses OS detection from remote command output.
func DetectOSFromOutput(osRelease, arch string) *OSInfo {
	info := &OSInfo{
		Arch: arch,
	}

	info.Distro, info.Version = parseOSRelease(osRelease)

	if info.Distro == Unknown {
		info.Distro, info.Version = parseLSBRelease(osRelease)
	}

	return info
}

func parseOSRelease(data string) (OSDistro, string) {
	lines := strings.Split(data, "\n")
	var id, versionID string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), `"`)
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			versionID = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"`)
		}
	}

	switch id {
	case "ubuntu":
		return Ubuntu, versionID
	case "debian":
		return Debian, versionID
	case "centos":
		return CentOS, versionID
	case "fedora":
		return Fedora, versionID
	case "rocky":
		return RockyLinux, versionID
	case "almalinux":
		return AlmaLinux, versionID
	case "rhel":
		return RHEL, versionID
	case "amzn":
		return AmazonLinux, versionID
	case "arch":
		return Arch, versionID
	}

	return Unknown, versionID
}

func parseLSBRelease(data string) (OSDistro, string) {
	lines := strings.Split(data, "\n")
	var distro, version string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DISTRIB_ID=") {
			distro = strings.Trim(strings.TrimPrefix(line, "DISTRIB_ID="), `"`)
		}
		if strings.HasPrefix(line, "DISTRIB_RELEASE=") {
			version = strings.Trim(strings.TrimPrefix(line, "DISTRIB_RELEASE="), `"`)
		}
	}

	switch strings.ToLower(distro) {
	case "ubuntu":
		return Ubuntu, version
	case "debian":
		return Debian, version
	}

	return Unknown, version
}

func parseRedHatRelease(data string) (OSDistro, string) {
	data = strings.ToLower(data)

	if strings.Contains(data, "centos") {
		parts := strings.Fields(data)
		for i, part := range parts {
			if strings.HasPrefix(part, "release") && i+1 < len(parts) {
				return CentOS, strings.Split(parts[i+1], ".")[0]
			}
		}
		return CentOS, ""
	}

	if strings.Contains(data, "rocky") {
		return RockyLinux, ""
	}

	if strings.Contains(data, "alma") {
		return AlmaLinux, ""
	}

	if strings.Contains(data, "red hat") || strings.Contains(data, "rhel") {
		return RHEL, ""
	}

	if strings.Contains(data, "fedora") {
		return Fedora, ""
	}

	return Unknown, ""
}

// IsDebianBased reports whether this is a Debian-based distribution.
func (o *OSInfo) IsDebianBased() bool {
	switch o.Distro {
	case Ubuntu, Debian:
		return true
	default:
		return false
	}
}

// IsRHELBased reports whether this is a RHEL-based distribution.
func (o *OSInfo) IsRHELBased() bool {
	switch o.Distro {
	case CentOS, Fedora, RockyLinux, AlmaLinux, RHEL, AmazonLinux:
		return true
	default:
		return false
	}
}

// IsSupported reports whether the OS is explicitly supported for bootstrapping.
func (o *OSInfo) IsSupported() bool {
	switch o.Distro {
	case Ubuntu, Debian, CentOS, RockyLinux, AlmaLinux, Fedora, AmazonLinux:
		return true
	default:
		return false
	}
}

// GetUnsupportedMessage returns an advisory message for unsupported distributions.
func (o *OSInfo) GetUnsupportedMessage() string {
	return fmt.Sprintf("OS '%s' is not explicitly supported. Bootstrap may still work but hasn't been tested.\n"+
		"Supported OS: Ubuntu (18.04+), Debian (10+), CentOS (7+), Rocky Linux (8+), AlmaLinux (8+), Fedora (33+)",
		o.Distro)
}
