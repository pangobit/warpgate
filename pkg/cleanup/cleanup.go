// Package cleanup removes Warpgate dependencies from bootstrapped nodes.
package cleanup

import (
	"fmt"
	"os"
	"strings"

	"github.com/pangobit/warpgate/pkg/bootstrap"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/ssh"
	"github.com/pangobit/warpgate/pkg/tui"
)

// Cleaner handles node cleanup operations.
type Cleaner struct {
	// Config is the cluster configuration.
	Config *config.ClusterConfig
	// SSHKey is the path to the SSH private key.
	SSHKey string
	// TailscaleSSH uses Tailscale SSH instead of key-based auth.
	TailscaleSSH bool
	// RemoveGo removes the Go installation.
	RemoveGo bool
	// RemoveDocker removes Docker entirely.
	RemoveDocker bool
}

// NewCleaner creates a new cleaner instance.
func NewCleaner(cfg *config.ClusterConfig, sshKey string) *Cleaner {
	return &Cleaner{
		Config: cfg,
		SSHKey: sshKey,
	}
}

// CleanupNode cleans up a specific node by its config ID.
func (c *Cleaner) CleanupNode(nodeID string) error {
	node := c.Config.GetNode(nodeID)
	if node == nil {
		return fmt.Errorf("node '%s' not found in configuration", nodeID)
	}

	return c.cleanupNode(node, "")
}

// CleanupHost cleans up a node by host address in ad-hoc mode.
func (c *Cleaner) CleanupHost(host, user string) error {
	node := &config.NodeConfig{
		ID:   host,
		Host: host,
	}

	return c.cleanupNode(node, user)
}

func (c *Cleaner) cleanupNode(node *config.NodeConfig, user string) error {
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "root"
	}

	var client *ssh.Client
	if c.TailscaleSSH {
		client = ssh.NewTailscaleClient(node.Host, user)
	} else {
		var err error
		client, err = ssh.NewClient(node.Host, user, c.SSHKey)
		if err != nil {
			return fmt.Errorf("failed to create SSH client: %w", err)
		}
	}
	defer client.Close()

	fmt.Printf("Connecting to %s...\n", node.Host)
	if err := client.Connect(); err != nil {
		return err
	}

	title := fmt.Sprintf("Cleaning up %s (%s)", node.ID, node.Host)
	steps := c.buildSteps(client)

	return tui.Run(title, steps)
}

func (c *Cleaner) buildSteps(client *ssh.Client) []tui.StepDef {
	var osInfo *bootstrap.OSInfo

	steps := []tui.StepDef{
		{
			Name: "Detecting operating system",
			Run: func() (string, error) {
				info, err := detectOS(client)
				if err != nil {
					return "", err
				}
				osInfo = info
				return fmt.Sprintf("%s %s", info.Distro, info.Version), nil
			},
		},
		{
			Name: "Stopping app compose stacks",
			Run: func() (string, error) {
				return runScript(client, `
for dir in /opt/warpgate/apps/*/; do
    if [ -f "$dir/compose.yml" ]; then
        cd "$dir" && docker compose down 2>/dev/null || true
    fi
done`)
			},
		},
		{
			Name: "Stopping Traefik",
			Run: func() (string, error) {
				return runScript(client, `
if [ -f /opt/warpgate/traefik/compose.yml ]; then
    cd /opt/warpgate/traefik && docker compose down 2>/dev/null || true
fi`)
			},
		},
		{
			Name: "Stopping SecretSauce server",
			Run: func() (string, error) {
				return runScript(client, `
sudo systemctl stop secretsauce 2>/dev/null || true
sudo systemctl disable secretsauce 2>/dev/null || true
sudo rm -f /etc/systemd/system/secretsauce.service
sudo systemctl daemon-reload 2>/dev/null || true`)
			},
		},
		{
			Name: "Removing Warpgate directories",
			Run: func() (string, error) {
				return runScript(client, `
sudo mkdir -p /opt/warpgate
sudo rm -rf /opt/warpgate/apps /opt/warpgate/traefik /opt/warpgate/internal-proxy /opt/warpgate/bootstrap
sudo find /opt/warpgate -mindepth 1 -maxdepth 1 ! -name secretsauce -exec rm -rf {} + 2>/dev/null || true`)
			},
		},
		{
			Name: "Removing warpgate Docker network",
			Run: func() (string, error) {
				return runScript(client, "docker network rm warpgate 2>/dev/null || true")
			},
		},
		{
			Name: "Removing warpgate user",
			Run: func() (string, error) {
				return runScript(client, `
sudo userdel -r warpgate 2>/dev/null || true
sudo rm -f /usr/local/bin/secretsauce`)
			},
		},
	}

	if c.RemoveGo {
		steps = append(steps, tui.StepDef{
			Name: "Removing Go installation",
			Run: func() (string, error) {
				return runScript(client, "sudo rm -rf /usr/local/go")
			},
		})
	}

	if c.RemoveDocker {
		steps = append(steps, tui.StepDef{
			Name: "Removing Docker",
			Run: func() (string, error) {
				if osInfo != nil && osInfo.IsDebianBased() {
					return runScript(client, `
sudo systemctl stop docker 2>/dev/null || true
sudo apt-get remove -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin 2>/dev/null || true
sudo apt-get autoremove -y 2>/dev/null || true`)
				}
				if osInfo != nil && osInfo.IsRHELBased() {
					return runScript(client, `
sudo systemctl stop docker 2>/dev/null || true
sudo yum remove -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin 2>/dev/null || true`)
				}
				return runScript(client, `
sudo systemctl stop docker 2>/dev/null || true
echo "Docker removal: unknown distro, please remove manually"`)
			},
		})
	}

	return steps
}

func detectOS(client *ssh.Client) (*bootstrap.OSInfo, error) {
	arch, _, err := client.RunCommand("uname -m")
	if err != nil {
		return nil, fmt.Errorf("failed to detect arch: %w", err)
	}

	stdout, _, err := client.RunCommand("cat /etc/os-release 2>/dev/null || echo 'NOT_FOUND'")
	if err != nil || strings.TrimSpace(stdout) == "NOT_FOUND" {
		stdout, _, _ = client.RunCommand("cat /etc/lsb-release 2>/dev/null || echo 'NOT_FOUND'")
	}

	return bootstrap.DetectOSFromOutput(stdout, strings.TrimSpace(arch)), nil
}

func runScript(client *ssh.Client, script string) (string, error) {
	wrapped := "#!/bin/bash\nset -e\n\n" + script
	_, err := client.RunScriptSilent(wrapped)
	return "", err
}
