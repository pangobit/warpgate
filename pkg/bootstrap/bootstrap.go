// Package bootstrap handles node provisioning via SSH.
package bootstrap

import (
	"fmt"
	"os"

	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/ssh"
	"github.com/sirupsen/logrus"
)

// Bootstrapper handles node bootstrap operations.
type Bootstrapper struct {
	// Config is the cluster configuration.
	Config *config.ClusterConfig
	// SSHKey is the path to the SSH private key (empty when using Tailscale SSH).
	SSHKey string
	// TailscaleSSH uses the ssh binary with Tailscale auth instead of key-based auth.
	TailscaleSSH bool
	// DryRun prints the install script without executing it.
	DryRun bool
	// Verbose enables verbose logging.
	Verbose bool

	log *logrus.Logger
}

// NewBootstrapper creates a new bootstrapper instance.
func NewBootstrapper(cfg *config.ClusterConfig, sshKey string) *Bootstrapper {
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		ForceColors: true,
	})

	return &Bootstrapper{
		Config: cfg,
		SSHKey: sshKey,
		log:    log,
	}
}

// BootstrapNode bootstraps a specific node by its config ID.
func (b *Bootstrapper) BootstrapNode(nodeID string) error {
	node := b.Config.GetNode(nodeID)
	if node == nil {
		return fmt.Errorf("node '%s' not found in configuration", nodeID)
	}

	return b.bootstrapNode(node, "")
}

// BootstrapHost bootstraps a node by host address in ad-hoc mode.
func (b *Bootstrapper) BootstrapHost(host, user string) error {
	node := &config.NodeConfig{
		ID:   host,
		Host: host,
	}

	return b.bootstrapNode(node, user)
}

func (b *Bootstrapper) bootstrapNode(node *config.NodeConfig, user string) error {
	b.log.Infof("Bootstrapping node: %s (%s)", node.ID, node.Host)

	if b.DryRun {
		osInfo := &OSInfo{
			Distro:  Ubuntu,
			Version: "22.04",
			Arch:    "amd64",
		}
		script := osInfo.InstallScript(b.Config.GoProxy, &b.Config.Networking)
		b.log.Info("DRY RUN - Sample script that would be generated after OS detection:")
		fmt.Println("=====================================")
		fmt.Println(script)
		fmt.Println("=====================================")
		b.log.Info("Dry run complete - actual OS would be detected on the remote node")
		return nil
	}

	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "root"
	}

	var client *ssh.Client
	if b.TailscaleSSH {
		client = ssh.NewTailscaleClient(node.Host, user)
	} else {
		var err error
		client, err = ssh.NewClient(node.Host, user, b.SSHKey)
		if err != nil {
			return fmt.Errorf("failed to create SSH client: %w", err)
		}
	}

	b.log.Info("Connecting to node...")
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()
	b.log.Info("Connected successfully")

	b.log.Info("Detecting operating system...")
	osInfo, err := b.detectOS(client)
	if err != nil {
		return fmt.Errorf("failed to detect OS: %w", err)
	}

	b.log.Infof("Detected OS: %s %s (%s)", osInfo.Distro, osInfo.Version, osInfo.Arch)

	if !osInfo.IsSupported() {
		b.log.Warn(osInfo.GetUnsupportedMessage())
	}

	script := osInfo.InstallScript(b.Config.GoProxy, &b.Config.Networking)

	b.log.Info("Running installation script...")
	b.log.Info("This may take a few minutes depending on network speed")

	if err := client.RunScript(script); err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	b.log.Infof("Node %s bootstrapped successfully!", node.ID)
	b.log.Info("Next steps:")
	b.log.Info("1. Deploy apps: warpgate deploy <app>")

	return nil
}

func (b *Bootstrapper) detectOS(client *ssh.Client) (*OSInfo, error) {
	arch, _, err := client.RunCommand("uname -m")
	if err != nil {
		return nil, fmt.Errorf("failed to detect arch: %w", err)
	}

	stdout, _, err := client.RunCommand("cat /etc/os-release 2>/dev/null || echo 'NOT_FOUND'")
	if err != nil || stdout == "NOT_FOUND\n" {
		stdout, _, _ = client.RunCommand("cat /etc/lsb-release 2>/dev/null || echo 'NOT_FOUND'")
	}

	return DetectOSFromOutput(stdout, arch), nil
}

// ValidatePrerequisites checks that the local machine has SSH installed.
func (b *Bootstrapper) ValidatePrerequisites() error {
	if _, err := os.Stat("/usr/bin/ssh"); err != nil {
		if _, err := os.Stat("/usr/local/bin/ssh"); err != nil {
			return fmt.Errorf("ssh command not found - please install OpenSSH client")
		}
	}

	return nil
}
