// Package bootstrap handles node provisioning via SSH.
package bootstrap

import (
	"fmt"
	"os"
	"strings"

	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/ssh"
	"github.com/pangobit/warpgate/pkg/tui"
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
}

// NewBootstrapper creates a new bootstrapper instance.
func NewBootstrapper(cfg *config.ClusterConfig, sshKey string) *Bootstrapper {
	return &Bootstrapper{
		Config: cfg,
		SSHKey: sshKey,
	}
}

// BootstrapNode bootstraps a node using its full config (including PrivateIP).
func (b *Bootstrapper) BootstrapNode(node *config.NodeConfig, user string) error {
	return b.bootstrapNode(node, user)
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
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "root"
	}

	stepCfg := &StepConfig{
		GoProxy:        b.Config.GoProxy,
		Networking:     &b.Config.Networking,
		PrivateIP:      node.PrivateIP,
		MasterPassword: os.Getenv("SS_MASTER_PASSWORD"),
		Registry:       &b.Config.Registry,
	}

	if b.DryRun {
		fmt.Printf("DRY RUN — would bootstrap %s (%s):\n\n", node.ID, node.Host)
		for i, step := range BuildSteps(nil, stepCfg) {
			fmt.Printf("  %d. %s\n", i+1, step.Name)
		}
		return nil
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
	defer client.Close()

	fmt.Printf("Connecting to %s...\n", node.Host)
	if err := client.Connect(); err != nil {
		return err
	}

	title := fmt.Sprintf("Bootstrapping %s (%s)", node.ID, node.Host)
	steps := BuildSteps(client, stepCfg)

	if err := tui.Run(title, steps); err != nil {
		return err
	}

	if stepCfg.generatedPassword != "" {
		host := node.Host
		if node.PrivateIP != "" {
			host = node.PrivateIP
		}
		fmt.Println()
		fmt.Println("  ┌──────────────────────────────────────────────────────────────┐")
		fmt.Println("  │ SecretSauce master password (save this — shown only once):   │")
		fmt.Printf("  │   %s%s│\n", stepCfg.generatedPassword, strings.Repeat(" ", 59-len(stepCfg.generatedPassword)))
		fmt.Println("  │                                                              │")
		fmt.Printf("  │ Web UI: http://%s:8090%s│\n", host, strings.Repeat(" ", 44-len(host)))
		fmt.Println("  └──────────────────────────────────────────────────────────────┘")
	}

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
