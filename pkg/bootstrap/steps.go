package bootstrap

import (
	"fmt"
	"strings"

	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/ssh"
	"github.com/pangobit/warpgate/pkg/tui"
)

// StepConfig holds the parameters needed to build bootstrap step scripts.
type StepConfig struct {
	// GoProxy is the private Go module proxy URL.
	GoProxy string
	// Networking is the cluster networking configuration.
	Networking *config.NetworkingConfig
	// TailscaleIP is the node's Tailscale IP for internal proxy binding.
	TailscaleIP string
	// MasterPassword is the SecretSauce master password for vault initialization.
	MasterPassword string

	// generatedPassword is set by setupSecretsServer when it auto-generates a password.
	generatedPassword string
}

// BuildSteps returns the TUI step definitions for bootstrapping a node.
func BuildSteps(client *ssh.Client, cfg *StepConfig) []tui.StepDef {
	var osInfo *OSInfo

	steps := []tui.StepDef{
		{
			Name: "Detecting operating system",
			Run: func() (string, error) {
				info, err := detectOSRemote(client)
				if err != nil {
					return "", err
				}
				osInfo = info
				return fmt.Sprintf("%s %s, %s", info.Distro, info.Version, info.Arch), nil
			},
		},
		{
			Name: "Creating warpgate user",
			Run: func() (string, error) {
				return createUser(client)
			},
		},
		{
			Name: "Installing Go",
			Run: func() (string, error) {
				return installGo(client, osInfo)
			},
		},
		{
			Name: "Installing Docker",
			Run: func() (string, error) {
				return installDocker(client, osInfo)
			},
		},
		{
			Name: "Configuring docker group",
			Run: func() (string, error) {
				return addDockerGroup(client)
			},
		},
		{
			Name: "Installing SecretSauce",
			Run: func() (string, error) {
				return installSecretSauce(client, cfg.GoProxy)
			},
		},
		{
			Name: "Setting up SSH keys",
			Run: func() (string, error) {
				return setupSSHKeys(client)
			},
		},
		{
			Name: "Setting up Warpgate + Traefik",
			Run: func() (string, error) {
				return setupWarpgate(client, cfg.Networking)
			},
		},
		{
			Name: "Setting up Internal Proxy",
			Run: func() (string, error) {
				return setupInternalProxy(client, cfg.TailscaleIP)
			},
		},
		{
			Name: "Setting up SecretSauce server",
			Run: func() (string, error) {
				detail, err := setupSecretsServer(client, cfg.MasterPassword)
				if err != nil {
					return "", err
				}
				if strings.HasPrefix(detail, "password:") {
					cfg.generatedPassword = strings.TrimPrefix(detail, "password:")
					return "initialized and started", nil
				}
				return detail, nil
			},
		},
	}

	return steps
}

func detectOSRemote(client *ssh.Client) (*OSInfo, error) {
	arch, _, err := client.RunCommand("uname -m")
	if err != nil {
		return nil, fmt.Errorf("failed to detect arch: %w", err)
	}
	arch = strings.TrimSpace(arch)

	stdout, _, err := client.RunCommand("cat /etc/os-release 2>/dev/null || echo 'NOT_FOUND'")
	if err != nil || strings.TrimSpace(stdout) == "NOT_FOUND" {
		stdout, _, _ = client.RunCommand("cat /etc/lsb-release 2>/dev/null || echo 'NOT_FOUND'")
	}

	return DetectOSFromOutput(stdout, arch), nil
}
