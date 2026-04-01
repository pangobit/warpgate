// Package bootstrap handles node provisioning via SSH.
package bootstrap

import (
	"fmt"
	"os"

	"github.com/pangobit/warpgate/pkg/config"
	"github.com/sirupsen/logrus"
)

// Bootstrapper handles node bootstrap operations.
type Bootstrapper struct {
	// Config is the cluster configuration.
	Config *config.ClusterConfig
	// SSHKey is the path to the SSH private key.
	SSHKey string
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
		script := osInfo.InstallScript(b.Config.GoProxy)
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

	client, err := NewSSHClient(node.Host, user, b.SSHKey)
	if err != nil {
		return fmt.Errorf("failed to create SSH client: %w", err)
	}

	b.log.Info("Connecting to node...")
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()
	b.log.Info("Connected successfully")

	b.log.Info("Detecting operating system...")
	osInfo, err := client.DetectOS()
	if err != nil {
		return fmt.Errorf("failed to detect OS: %w", err)
	}

	b.log.Infof("Detected OS: %s %s (%s)", osInfo.Distro, osInfo.Version, osInfo.Arch)

	if !osInfo.IsSupported() {
		b.log.Warn(osInfo.GetUnsupportedMessage())
	}

	script := osInfo.InstallScript(b.Config.GoProxy)

	b.log.Info("Running installation script...")
	b.log.Info("This may take a few minutes depending on network speed")

	if err := client.RunScript(script); err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	b.log.Infof("Node %s bootstrapped successfully!", node.ID)
	b.log.Info("Next steps:")
	b.log.Info("1. Copy the SSH public key to your authorized_keys on other nodes")
	b.log.Info("2. Test: warpgate status")
	b.log.Info("3. Deploy: warpgate deploy <app>")

	return nil
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
