// Package cli implements the warpgate CLI commands.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pangobit/warpgate/pkg/bootstrap"
	"github.com/pangobit/warpgate/pkg/compose"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/spf13/cobra"
)

var (
	configPath string
	cfg        *config.ClusterConfig
)

var rootCmd = &cobra.Command{
	Use:   "warpgate",
	Short: "Warpgate - Lightweight app deployment and orchestration",
	Long: `Warpgate is a lightweight deployment tool inspired by Starcraft's warpgate.

It provides a simpler alternative to k3s + Flux for deploying containerized applications
using Docker Compose, Traefik, Tailscale, and your own infrastructure.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "init" {
			return nil
		}

		if configPath == "" {
			configPath = "warpgate.yml"
		}

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s (run 'warpgate init' to create one)", configPath)
		}

		var err error
		cfg, err = config.LoadClusterConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to warpgate.yml config file")

	// Add commands
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(bootstrapCmd)
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

		configPath := "warpgate.yml"
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("warpgate.yml already exists")
		}

		defaultConfig := fmt.Sprintf(`version: "1"
project: %s

# Cluster nodes
nodes:
  - id: node-1
    host: 10.0.0.1
    tailscale_ip: 100.x.x.x
    roles: [control-plane, worker]

# Networking configuration
networking:
  tailnet: your-tailnet.ts.net
  dns:
    provider: cloudflare
    zone: example.com
  traefik:
    entry_points: [web, websecure]
    acme:
      enabled: true
      email: admin@example.com
      provider: letsencrypt

# Container registry
registry:
  server: ghcr.io

# Secrets provider
secrets:
  provider: secretsauce
  config:
    endpoint: https://secretsauce.internal

# Applications to deploy
apps:
  - name: example-app
    image: ghcr.io/org/example-app
    version: latest
    replicas: 1
    targets: [all]
    domains:
      - example-app.example.com
    ports:
      - container: 8080
    env:
      LOG_LEVEL: info
    secrets:
      - DATABASE_URL
    health_check:
      path: /health
      port: 8080
      interval: 10s
    # Optional: sidecar containers (run alongside the app)
    # sidecars:
    #   - name: litestream
    #     image: litestream/litestream:0.5.6
    #     volumes: [app-data:/data]
    # Optional: init containers (run before the app starts)
    # init:
    #   - name: restore
    #     image: litestream/litestream:0.5.6
    #     command: "litestream restore /data/app.db"
    #     volumes: [app-data:/data]
`, projectName)

		if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
			return fmt.Errorf("failed to create warpgate.yml: %w", err)
		}

		fmt.Printf("Created warpgate.yml for project '%s'\n", projectName)
		fmt.Println("\nNext steps:")
		fmt.Println("1. Edit warpgate.yml with your node details")
		fmt.Println("2. Run 'warpgate generate' to generate Docker Compose files")
		fmt.Println("3. Run 'warpgate deploy <app-name>' to deploy")

		return nil
	},
}

var deployCmd = &cobra.Command{
	Use:   "deploy <app-name> [version]",
	Short: "Deploy an application",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		version := ""
		if len(args) > 1 {
			version = args[1]
		}

		app := cfg.GetApp(appName)
		if app == nil {
			return fmt.Errorf("app '%s' not found in config", appName)
		}

		if version != "" {
			app.Version = version
		}

		fmt.Printf("Deploying %s:%s to targets: %v\n", app.Name, app.Version, app.GetTargetNodes(cfg.Nodes))

		// TODO: Implement actual deployment
		fmt.Println("Deployment command executed (implementation pending)")

		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show cluster and application status",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Project: %s\n", cfg.Project)
		fmt.Printf("Nodes: %d\n\n", len(cfg.Nodes))

		for _, node := range cfg.Nodes {
			roles := node.Roles
			if len(roles) == 0 {
				roles = []string{"control-plane", "worker"}
			}
			fmt.Printf("Node: %s\n", node.ID)
			fmt.Printf("  Host: %s\n", node.Host)
			fmt.Printf("  Roles: %v\n", roles)
			fmt.Printf("  Tailscale: %s\n", node.TailscaleIP)
			fmt.Println()
		}

		fmt.Printf("Apps: %d\n\n", len(cfg.Apps))
		for _, app := range cfg.Apps {
			fmt.Printf("App: %s\n", app.Name)
			fmt.Printf("  Image: %s:%s\n", app.Image, app.Version)
			fmt.Printf("  Targets: %v\n", app.GetTargetNodes(cfg.Nodes))
			fmt.Printf("  Replicas: %d\n", app.Replicas)
			if len(app.Domains) > 0 {
				fmt.Printf("  Domains: %v\n", app.Domains)
			}
			fmt.Println()
		}

		return nil
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs <app-name>",
	Short: "Stream logs from an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		fmt.Printf("Streaming logs for %s...\n", appName)
		// TODO: Implement log streaming
		return nil
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback <app-name>",
	Short: "Rollback an application to the previous version",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		fmt.Printf("Rolling back %s...\n", appName)
		// TODO: Implement rollback
		return nil
	},
}

var execCmd = &cobra.Command{
	Use:   "exec <app-name> <command>",
	Short: "Execute a command in an application container",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		command := args[1:]
		fmt.Printf("Executing %v in %s...\n", command, appName)
		// TODO: Implement exec
		return nil
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate [node-id]",
	Short: "Generate Docker Compose files for nodes",
	Long: `Generate a single docker-compose.yml per node containing all apps targeted at that node.
If a node ID is given, generates only for that node. Otherwise generates for all nodes.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var nodes []config.NodeConfig
		if len(args) > 0 {
			node := cfg.GetNode(args[0])
			if node == nil {
				return fmt.Errorf("node '%s' not found in config", args[0])
			}
			nodes = []config.NodeConfig{*node}
		} else {
			nodes = cfg.Nodes
		}

		for _, node := range nodes {
			apps := cfg.GetAppsForNode(node.ID)
			if len(apps) == 0 {
				fmt.Printf("No apps targeted at node %s, skipping\n", node.ID)
				continue
			}

			n := node
			project := compose.NewProject(cfg.Project, apps, &n, cfg.Networking)
			composeYAML, err := project.Generate()
			if err != nil {
				return fmt.Errorf("failed to generate compose for node %s: %w", node.ID, err)
			}

			outputPath := filepath.Join("generated", node.ID, "docker-compose.yml")
			if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
				return err
			}

			if err := os.WriteFile(outputPath, []byte(composeYAML), 0644); err != nil {
				return err
			}

			fmt.Printf("Generated %s (%d apps)\n", outputPath, len(apps))
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

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap [node-id]",
	Short: "Bootstrap a node with Warpgate dependencies",
	Long: `Bootstrap a node by installing required dependencies:
- Go (1.26.1)
- Docker (distro packages)
- Docker Compose (plugin)
- SecretSauce (if go_proxy configured)
- warpgate user with proper permissions

The node must have Tailscale and SSH already configured.

Examples:
  # Bootstrap node via Tailscale SSH (recommended)
  warpgate bootstrap test-node --tailscale-ssh

  # Bootstrap by IP (ad-hoc, Tailscale SSH)
  warpgate bootstrap --host 100.95.115.81 --tailscale-ssh

  # Bootstrap with SSH key
  warpgate bootstrap node-1 --ssh-key ~/.ssh/id_rsa

  # Dry run (show script without executing)
  warpgate bootstrap node-1 --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !bootstrapTailscaleSSH {
			if bootstrapSSHKey == "" {
				homeDir, _ := os.UserHomeDir()
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
		if cfg != nil {
			bs = bootstrap.NewBootstrapper(cfg, bootstrapSSHKey)
		} else {
			minimalCfg := &config.ClusterConfig{
				Nodes: []config.NodeConfig{},
			}
			bs = bootstrap.NewBootstrapper(minimalCfg, bootstrapSSHKey)
		}
		bs.DryRun = bootstrapDryRun
		bs.TailscaleSSH = bootstrapTailscaleSSH

		if len(args) > 0 {
			if cfg == nil {
				return fmt.Errorf("config file required to bootstrap by node ID — provide with -c flag")
			}
			node := cfg.GetNode(args[0])
			if node == nil {
				return fmt.Errorf("node '%s' not found in config", args[0])
			}
			return bs.BootstrapHost(node.Host, bootstrapUser)
		} else if bootstrapHost != "" {
			return bs.BootstrapHost(bootstrapHost, bootstrapUser)
		}

		return fmt.Errorf("specify node-id from config, or use --host for ad-hoc bootstrapping")
	},
}

func init() {
	bootstrapCmd.Flags().StringVar(&bootstrapHost, "host", "", "Target host IP or hostname (ad-hoc mode)")
	bootstrapCmd.Flags().StringVar(&bootstrapUser, "user", "", "SSH user (defaults to current user)")
	bootstrapCmd.Flags().StringVar(&bootstrapSSHKey, "ssh-key", "", "Path to SSH private key")
	bootstrapCmd.Flags().BoolVar(&bootstrapDryRun, "dry-run", false, "Show installation script without executing")
	bootstrapCmd.Flags().BoolVar(&bootstrapTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH (no key needed)")
}

// Execute runs the root cobra command.
func Execute() error {
	return rootCmd.Execute()
}
