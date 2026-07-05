// Package cli implements the warpgate CLI commands.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pangobit/warpgate/pkg/bootstrap"
	"github.com/pangobit/warpgate/pkg/cleanup"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/deploy"
	"github.com/pangobit/warpgate/pkg/upgrade"
	"github.com/pangobit/warpgate/pkg/version"
	"github.com/pangobit/warpgate/warpd"
	"github.com/spf13/cobra"
)

var (
	configPath string
	repo       *config.RepoConfig
)

var rootCmd = &cobra.Command{
	Use:   "warpgate",
	Short: "Warpgate - Lightweight app deployment and orchestration",
	Long: `Warpgate is a lightweight deployment tool inspired by Starcraft's warpgate.

It provides a simpler alternative to k3s + Flux for deploying containerized applications
using Docker Compose, Traefik, Tailscale, and your own infrastructure.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "init" || cmd.Name() == "ui" || cmd.Name() == "serve" || cmd.Name() == "upgrade" || cmd.Name() == "version" {
			return nil
		}

		if configPath == "" {
			configPath = "cluster.yml"
		}

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s (run 'warpgate init' to create one)", configPath)
		}

		var err error
		repo, err = config.LoadRepo(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		return nil
	},
}

// Bootstrap flags
var (
	bootstrapHost         string
	bootstrapUser         string
	bootstrapSSHKey       string
	bootstrapDryRun       bool
	bootstrapTailscaleSSH bool
)

// Serve flags
var (
	serveTailscaleSSH bool
	serveSSHKey       string
	serveUser         string
)

// Cleanup flags
var (
	cleanupHost         string
	cleanupUser         string
	cleanupSSHKey       string
	cleanupTailscaleSSH bool
	cleanupForce        bool
	cleanupRemoveGo     bool
	cleanupRemoveDocker bool
)

// Shadow flags
var (
	shadowTailscaleSSH bool
	shadowSSHKey       string
	shadowUser         string
)

// Upgrade flags
var (
	upgradeVersion     string
	upgradeInstallPath string
	upgradeService     string
	upgradeNoRestart   bool
	upgradeDryRun      bool
)

// Setup registers all commands and flags on the root command tree.
// Called from Execute before running the CLI.
func Setup() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to cluster.yml config file")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(upgradeCmd)
	rootCmd.AddCommand(bootstrapCmd)
	rootCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(shadowCmd)

	shadowCmd.AddCommand(shadowDeployCmd)
	shadowCmd.AddCommand(shadowPromoteCmd)
	shadowCmd.AddCommand(shadowRemoveCmd)
	shadowCmd.AddCommand(shadowStatusCmd)

	serveCmd.Flags().BoolVar(&serveTailscaleSSH, "tailscale-ssh", true, "Use Tailscale SSH for node deploys")
	serveCmd.Flags().StringVar(&serveSSHKey, "ssh-key", "", "Path to SSH private key for node deploys")
	serveCmd.Flags().StringVar(&serveUser, "user", "", "SSH user for node deploys")

	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "latest", "Release tag to install, or latest")
	upgradeCmd.Flags().StringVar(&upgradeInstallPath, "install-path", "", "Binary install path (default: current executable)")
	upgradeCmd.Flags().StringVar(&upgradeService, "service", "warpgate", "Systemd service name without .service suffix")
	upgradeCmd.Flags().BoolVar(&upgradeNoRestart, "no-restart", false, "Install the binary without restarting systemd")
	upgradeCmd.Flags().BoolVar(&upgradeDryRun, "dry-run", false, "Show the planned upgrade without making changes")

	bootstrapCmd.Flags().StringVar(&bootstrapHost, "host", "", "Target host IP or hostname (ad-hoc mode)")
	bootstrapCmd.Flags().StringVar(&bootstrapUser, "user", "", "SSH user (defaults to current user)")
	bootstrapCmd.Flags().StringVar(&bootstrapSSHKey, "ssh-key", "", "Path to SSH private key")
	bootstrapCmd.Flags().BoolVar(&bootstrapDryRun, "dry-run", false, "Show installation script without executing")
	bootstrapCmd.Flags().BoolVar(&bootstrapTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH (no key needed)")

	cleanupCmd.Flags().StringVar(&cleanupHost, "host", "", "Target host IP or hostname (ad-hoc mode)")
	cleanupCmd.Flags().StringVar(&cleanupUser, "user", "", "SSH user (defaults to current user)")
	cleanupCmd.Flags().StringVar(&cleanupSSHKey, "ssh-key", "", "Path to SSH private key")
	cleanupCmd.Flags().BoolVar(&cleanupTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH (no key needed)")
	cleanupCmd.Flags().BoolVar(&cleanupForce, "force", false, "Skip confirmation prompt")
	cleanupCmd.Flags().BoolVar(&cleanupRemoveGo, "remove-go", false, "Also remove Go installation")
	cleanupCmd.Flags().BoolVar(&cleanupRemoveDocker, "remove-docker", false, "Also remove Docker")

	shadowDeployCmd.Flags().BoolVar(&shadowTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	shadowDeployCmd.Flags().StringVar(&shadowSSHKey, "ssh-key", "", "Path to SSH private key")
	shadowDeployCmd.Flags().StringVar(&shadowUser, "user", "", "SSH user (defaults to current user)")

	shadowPromoteCmd.Flags().BoolVar(&shadowTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	shadowPromoteCmd.Flags().StringVar(&shadowSSHKey, "ssh-key", "", "Path to SSH private key")
	shadowPromoteCmd.Flags().StringVar(&shadowUser, "user", "", "SSH user (defaults to current user)")

	shadowRemoveCmd.Flags().BoolVar(&shadowTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	shadowRemoveCmd.Flags().StringVar(&shadowSSHKey, "ssh-key", "", "Path to SSH private key")
	shadowRemoveCmd.Flags().StringVar(&shadowUser, "user", "", "SSH user (defaults to current user)")

	shadowStatusCmd.Flags().BoolVar(&shadowTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	shadowStatusCmd.Flags().StringVar(&shadowSSHKey, "ssh-key", "", "Path to SSH private key")
	shadowStatusCmd.Flags().StringVar(&shadowUser, "user", "", "SSH user (defaults to current user)")
}

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new Warpgate project",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectName := "myapp"
		if len(args) > 0 {
			projectName = args[0]
		}

		if _, err := os.Stat("cluster.yml"); err == nil {
			return fmt.Errorf("cluster.yml already exists")
		}

		clusterConfig := fmt.Sprintf(`version: "2"
project: %s

nodes:
  - id: node-1
    host: 10.0.0.1
    private_ip: 100.x.x.x

networking:
  private_network: your-network.ts.net
  dns:
    provider: cloudflare
    zone: example.com
    api_token: ${CF_DNS_API_TOKEN}
  traefik:
    entry_points: [web, websecure]
    acme:
      enabled: true
      email: admin@example.com
      provider: letsencrypt
      challenge: dns

registry:
  server: ghcr.io
  # Credentials are stored in SecretSauce during bootstrap.
  # Set REGISTRY_USERNAME and REGISTRY_TOKEN env vars when running bootstrap.

secrets:
  server: http://100.x.x.x:8090    # SecretSauce server URL on private network
`, projectName)

		if err := os.WriteFile("cluster.yml", []byte(clusterConfig), 0644); err != nil {
			return fmt.Errorf("failed to create cluster.yml: %w", err)
		}

		exampleDir := filepath.Join("apps", "example-app")
		if err := os.MkdirAll(exampleDir, 0755); err != nil {
			return fmt.Errorf("failed to create apps directory: %w", err)
		}

		appConfig := `kind: warpgate/app
release:
  services:
    example-app:
      image: ghcr.io/org/example-app
      image_tag: latest
      secrets_prefix: example-app/prod
      port: 8080
      environment:
        LOG_LEVEL: info
      expose:
        public:
          domains: [example-app.example.com]
        private:
          port: 8080
`
		if err := os.WriteFile(filepath.Join(exampleDir, "app.yml"), []byte(appConfig), 0644); err != nil {
			return fmt.Errorf("failed to create app.yml: %w", err)
		}

		composeConfig := `services:
  example-app:
    image: ghcr.io/org/example-app
    restart: unless-stopped
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
`
		if err := os.WriteFile(filepath.Join(exampleDir, "compose.yml"), []byte(composeConfig), 0644); err != nil {
			return fmt.Errorf("failed to create compose.yml: %w", err)
		}

		fmt.Printf("Created project '%s'\n", projectName)
		fmt.Println("\nFiles created:")
		fmt.Println("  cluster.yml              - Cluster configuration")
		fmt.Println("  apps/example-app/app.yml - Example app deployment config")
		fmt.Println("  apps/example-app/compose.yml - Example Docker Compose file")
		fmt.Println("\nNext steps:")
		fmt.Println("1. Edit cluster.yml with your node details")
		fmt.Println("2. Edit apps/example-app/ or create new app directories")
		fmt.Println("3. Run 'warpgate bootstrap <node-id>' to prepare a node")
		fmt.Println("4. Run 'warpgate deploy <app-name>' to deploy")

		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Warpgate version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("warpgate %s (%s)\n", version.Current(), version.Platform())
		return nil
	},
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the Warpgate daemon binary",
	Long: `Download a release binary from GitHub Releases, replace the installed
warpgate binary, and restart the systemd service.

Production hosts usually install the daemon to /usr/local/bin/warpgate and run
it as warpgate.service. Use sudo when the install path or systemd unit requires
root privileges.

Set GITHUB_TOKEN or GH_TOKEN when downloading from a private repository.

Examples:
  sudo warpgate upgrade
  sudo warpgate upgrade --version v1.2.3
  warpgate upgrade --dry-run`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		upgrader := upgrade.Upgrader{}
		return upgrader.Run(context.Background(), upgrade.Options{
			Version:     upgradeVersion,
			InstallPath: upgradeInstallPath,
			ServiceName: upgradeService,
			NoRestart:   upgradeNoRestart,
			DryRun:      upgradeDryRun,
		})
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the Warpgate daemon",
	Long: `Run the Warpgate daemon: watch the desired-state repository, commit
semver image bumps, serve the operator TUI over SSH, and expose a lean HTTP
API for CI.

Configuration comes from the environment:
  WARPGATE_REPO                  desired-state repository as owner/repo (required on first run)
  WARPGATE_REPO_BRANCH           branch to watch (default: master)
  WARPGATE_REPO_PATH             optional repository subdirectory
  WARPGATE_GH_APP_ID             GitHub App ID
  WARPGATE_GH_INSTALLATION_ID    GitHub App installation ID
  WARPGATE_GH_PRIVATE_KEY_FILE   path to the GitHub App private key PEM
  WARPGATE_REGISTRY_TOKEN        classic PAT with read:packages for GHCR reads
                                 (GHCR does not accept GitHub App tokens; without
                                 this, only public images can be watched)
  WARPGATE_HTTP_ADDR             CI API listen address (default: 127.0.0.1:7411)
  WARPGATE_SSH_ADDR              operator TUI SSH listen address (default: 127.0.0.1:7422)
  WARPGATE_HOST_KEY              daemon SSH host key path (generated when missing)
  WARPGATE_DB_PATH               daemon database path

Bind WARPGATE_HTTP_ADDR and WARPGATE_SSH_ADDR to a tailnet address; Tailscale
ACLs are the access control layer for both.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := warpd.DefaultServeConfig()
		cfg.Deploy.TailscaleSSH = serveTailscaleSSH
		cfg.Deploy.SSHKey = serveSSHKey
		cfg.Deploy.User = serveUser
		return warpd.RunServe(context.Background(), cfg)
	},
}

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap [node-id]",
	Short: "Bootstrap a node with Warpgate dependencies",
	Long: `Bootstrap a node by installing required dependencies:
- Docker and Docker Compose plugin
- Go and SecretSauce (if go_proxy configured)
- Traefik reverse proxy
- warpgate user with proper permissions
- SecretSauce server as systemd service

The node must have Tailscale and SSH already configured.

During bootstrap, the SecretSauce vault is automatically initialized or reused:
- If SS_MASTER_PASSWORD is set, that password is used
- Otherwise, a strong password is auto-generated and displayed once
- The master key file is created and the service is started or restarted
- Manage secrets via the SecretSauce web UI at http://<node-ip>:8090

Examples:
  warpgate bootstrap test-node --tailscale-ssh
  SS_MASTER_PASSWORD=secret warpgate bootstrap node-1 --tailscale-ssh
  CF_DNS_API_TOKEN=token warpgate bootstrap node-1 --tailscale-ssh
  warpgate bootstrap --host 100.95.115.81 --tailscale-ssh
  warpgate bootstrap node-1 --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !bootstrapTailscaleSSH {
			if bootstrapSSHKey == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to determine home directory: %w", err)
				}
				for _, key := range []string{
					filepath.Join(homeDir, ".ssh", "id_ed25519"),
					filepath.Join(homeDir, ".ssh", "id_rsa"),
				} {
					if _, err := os.Stat(key); err == nil {
						bootstrapSSHKey = key
						break
					}
				}
			}

			if bootstrapSSHKey == "" {
				return fmt.Errorf("no SSH key found — use --ssh-key or --tailscale-ssh")
			}

			if _, err := os.Stat(bootstrapSSHKey); err != nil {
				return fmt.Errorf("SSH key not found: %s", bootstrapSSHKey)
			}
		}

		var bs *bootstrap.Bootstrapper
		if repo != nil {
			bs = bootstrap.NewBootstrapper(repo.Cluster, bootstrapSSHKey)
		} else {
			minimalCfg := &config.ClusterConfig{
				Nodes: []config.NodeConfig{},
			}
			bs = bootstrap.NewBootstrapper(minimalCfg, bootstrapSSHKey)
		}
		bs.DryRun = bootstrapDryRun
		bs.TailscaleSSH = bootstrapTailscaleSSH

		if len(args) > 0 {
			if repo == nil {
				return fmt.Errorf("config file required to bootstrap by node ID — provide with -c flag")
			}
			node := repo.Cluster.GetNode(args[0])
			if node == nil {
				return fmt.Errorf("node '%s' not found in config", args[0])
			}
			return bs.BootstrapNode(node, bootstrapUser)
		} else if bootstrapHost != "" {
			return bs.BootstrapHost(bootstrapHost, bootstrapUser)
		}

		return fmt.Errorf("specify node-id from config, or use --host for ad-hoc bootstrapping")
	},
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [node-id]",
	Short: "Remove Warpgate dependencies from a node",
	Long: `Remove all Warpgate-installed dependencies from a node:
- Stop and remove app and Traefik compose stacks
- Remove /opt/warpgate/ directory
- Remove warpgate Docker network and user

Optional:
  --remove-go      Also remove Go installation
  --remove-docker  Also remove Docker (use with caution)

Examples:
  warpgate cleanup test-node --tailscale-ssh
  warpgate cleanup --host 100.95.115.81 --tailscale-ssh --force
  warpgate cleanup test-node --tailscale-ssh --remove-go --remove-docker`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cleanupTailscaleSSH {
			if cleanupSSHKey == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to determine home directory: %w", err)
				}
				for _, key := range []string{
					filepath.Join(homeDir, ".ssh", "id_ed25519"),
					filepath.Join(homeDir, ".ssh", "id_rsa"),
				} {
					if _, err := os.Stat(key); err == nil {
						cleanupSSHKey = key
						break
					}
				}
			}

			if cleanupSSHKey == "" {
				return fmt.Errorf("no SSH key found — use --ssh-key or --tailscale-ssh")
			}
		}

		if !cleanupForce {
			fmt.Println("This will remove Warpgate dependencies from the target node.")
			if cleanupRemoveGo {
				fmt.Println("  - Go installation will be removed")
			}
			if cleanupRemoveDocker {
				fmt.Println("  - Docker will be removed (other services may depend on it)")
			}
			fmt.Print("\nContinue? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		var cl *cleanup.Cleaner
		if repo != nil {
			cl = cleanup.NewCleaner(repo.Cluster, cleanupSSHKey)
		} else {
			minimalCfg := &config.ClusterConfig{
				Nodes: []config.NodeConfig{},
			}
			cl = cleanup.NewCleaner(minimalCfg, cleanupSSHKey)
		}
		cl.TailscaleSSH = cleanupTailscaleSSH
		cl.RemoveGo = cleanupRemoveGo
		cl.RemoveDocker = cleanupRemoveDocker

		if len(args) > 0 {
			if repo == nil {
				return fmt.Errorf("config file required to cleanup by node ID — provide with -c flag")
			}
			node := repo.Cluster.GetNode(args[0])
			if node == nil {
				return fmt.Errorf("node '%s' not found in config", args[0])
			}
			return cl.CleanupHost(node.Host, cleanupUser)
		} else if cleanupHost != "" {
			return cl.CleanupHost(cleanupHost, cleanupUser)
		}

		return fmt.Errorf("specify node-id from config, or use --host for ad-hoc cleanup")
	},
}

var shadowCmd = &cobra.Command{
	Use:   "shadow",
	Short: "Manage shadow deployments for pre-release testing",
	Long: `Shadow deploys a version of an app alongside the live deployment,
accessible only over the internal (Tailscale) network. Shadows are not wired
to the public proxy.

Examples:
  warpgate shadow deploy client v2.0
  warpgate shadow status client
  warpgate shadow promote client
  warpgate shadow remove client`,
}

var shadowDeployCmd = &cobra.Command{
	Use:   "deploy <app-name> <version>",
	Short: "Deploy a shadow version alongside the live deployment",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, shadowSSHKey)
		d.TailscaleSSH = shadowTailscaleSSH
		d.User = shadowUser
		d.GitHubToken = os.Getenv("GITHUB_TOKEN")
		return d.ShadowDeploy(args[0], args[1])
	},
}

var shadowPromoteCmd = &cobra.Command{
	Use:   "promote <app-name>",
	Short: "Promote a shadow deployment to live",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, shadowSSHKey)
		d.TailscaleSSH = shadowTailscaleSSH
		d.User = shadowUser
		d.GitHubToken = os.Getenv("GITHUB_TOKEN")
		return d.ShadowPromote(args[0])
	},
}

var shadowRemoveCmd = &cobra.Command{
	Use:   "remove <app-name>",
	Short: "Remove a shadow deployment and its companions",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, shadowSSHKey)
		d.TailscaleSSH = shadowTailscaleSSH
		d.User = shadowUser
		return d.ShadowRemove(args[0])
	},
}

var shadowStatusCmd = &cobra.Command{
	Use:   "status [app-name]",
	Short: "Show shadow deployment status",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return showShadowClusterStatus()
		}

		d := deploy.NewDeployer(repo, shadowSSHKey)
		d.TailscaleSSH = shadowTailscaleSSH
		d.User = shadowUser

		statuses, err := d.ShadowStatus(args[0])
		if err != nil {
			return err
		}

		fmt.Printf("Shadow: %s\n\n", args[0])
		for _, s := range statuses {
			if s.Error != "" {
				fmt.Printf("  %s: error — %s\n", s.NodeID, s.Error)
				continue
			}
			if s.Version == "" {
				fmt.Printf("  %s: no shadow\n", s.NodeID)
				continue
			}
			fmt.Printf("  %s: %s (version: %s)\n", s.NodeID, s.State, s.Version)
			if s.Containers != "" {
				for _, line := range strings.Split(s.Containers, "\n") {
					if line != "" {
						fmt.Printf("    %s\n", line)
					}
				}
			}
		}
		return nil
	},
}

// showShadowClusterStatus shows shadow status across all apps.
func showShadowClusterStatus() error {
	d := deploy.NewDeployer(repo, shadowSSHKey)
	d.TailscaleSSH = shadowTailscaleSSH
	d.User = shadowUser

	hasShadow := false
	for _, app := range repo.Apps {
		statuses, err := d.ShadowStatus(app.Name)
		if err != nil {
			continue
		}
		for _, s := range statuses {
			if s.Version != "" {
				if !hasShadow {
					fmt.Println("Active shadows:")
					hasShadow = true
				}
				fmt.Printf("  %s on %s: %s (version: %s)\n", app.Name, s.NodeID, s.State, s.Version)
			}
		}
	}

	if !hasShadow {
		fmt.Println("No active shadow deployments.")
	}
	return nil
}

// Execute registers all commands and runs the root cobra command.
func Execute() error {
	Setup()
	return rootCmd.Execute()
}
