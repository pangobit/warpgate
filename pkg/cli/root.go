// Package cli implements the warpgate CLI commands.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pangobit/warpgate/pkg/bootstrap"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/deploy"
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
		if cmd.Name() == "init" {
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

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to cluster.yml config file")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(execCmd)
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

		if _, err := os.Stat("cluster.yml"); err == nil {
			return fmt.Errorf("cluster.yml already exists")
		}

		clusterConfig := fmt.Sprintf(`version: "2"
project: %s

nodes:
  - id: node-1
    host: 10.0.0.1
    tailscale_ip: 100.x.x.x

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

registry:
  server: ghcr.io
`, projectName)

		if err := os.WriteFile("cluster.yml", []byte(clusterConfig), 0644); err != nil {
			return fmt.Errorf("failed to create cluster.yml: %w", err)
		}

		exampleDir := filepath.Join("apps", "example-app")
		if err := os.MkdirAll(exampleDir, 0755); err != nil {
			return fmt.Errorf("failed to create apps directory: %w", err)
		}

		appConfig := `image: ghcr.io/org/example-app
version: latest
domains:
  - example-app.example.com
secrets_prefix: example-app/prod
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
    environment:
      LOG_LEVEL: info
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

// Deploy flags
var (
	deployDryRun       bool
	deployTailscaleSSH bool
	deploySSHKey       string
)

var deployCmd = &cobra.Command{
	Use:   "deploy <app-name> [version]",
	Short: "Deploy an application to its target nodes",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		version := ""
		if len(args) > 1 {
			version = args[1]
		}

		d := deploy.NewDeployer(repo, deploySSHKey)
		d.TailscaleSSH = deployTailscaleSSH
		d.DryRun = deployDryRun

		return d.Deploy(appName, version)
	},
}

func init() {
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Show actions without executing")
	deployCmd.Flags().BoolVar(&deployTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	deployCmd.Flags().StringVar(&deploySSHKey, "ssh-key", "", "Path to SSH private key")
}

var statusCmd = &cobra.Command{
	Use:   "status [app-name]",
	Short: "Show cluster and application status",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Project: %s\n", repo.Cluster.Project)
		fmt.Printf("Nodes: %d\n\n", len(repo.Cluster.Nodes))

		for _, node := range repo.Cluster.Nodes {
			fmt.Printf("  %s (%s)", node.ID, node.Host)
			if node.TailscaleIP != "" {
				fmt.Printf(" [ts: %s]", node.TailscaleIP)
			}
			fmt.Println()
		}

		fmt.Printf("\nApps: %d\n\n", len(repo.Apps))
		for _, app := range repo.Apps {
			version := app.Version
			if version == "" {
				version = "latest"
			}
			fmt.Printf("  %s (%s:%s)\n", app.Name, app.Image, version)
			fmt.Printf("    Targets: %v\n", app.GetTargetNodes(repo.Cluster.Nodes))
			if len(app.Domains) > 0 {
				fmt.Printf("    Domains: %v\n", app.Domains)
			}
			if app.SecretsPrefix != "" {
				fmt.Printf("    Secrets: %s\n", app.SecretsPrefix)
			}
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
		// TODO: SSH to target nodes and run docker compose logs
		return nil
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback <app-name>",
	Short: "Rollback an application to the previous version",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, deploySSHKey)
		d.TailscaleSSH = deployTailscaleSSH
		return d.Rollback(args[0])
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
		// TODO: SSH to target node and run docker compose exec
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
- Go
- Docker and Docker Compose plugin
- SecretSauce (if go_proxy configured)
- Traefik reverse proxy
- warpgate user with proper permissions

The node must have Tailscale and SSH already configured.

Examples:
  warpgate bootstrap test-node --tailscale-ssh
  warpgate bootstrap --host 100.95.115.81 --tailscale-ssh
  warpgate bootstrap node-1 --ssh-key ~/.ssh/id_rsa
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
