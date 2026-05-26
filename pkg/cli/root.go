// Package cli implements the warpgate CLI commands.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pangobit/warpgate/pkg/bootstrap"
	"github.com/pangobit/warpgate/pkg/cleanup"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/deploy"
	"github.com/pangobit/warpgate/pkg/tui"
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
		if cmd.Name() == "init" || cmd.Name() == "ui" {
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

// Deploy flags
var (
	deployAll          bool
	deployDryRun       bool
	deployTailscaleSSH bool
	deploySSHKey       string
	deployUser         string
	deployReleaseID    string
)

// Remove flags
var (
	removeAll          bool
	removeForce        bool
	removeTailscaleSSH bool
	removeSSHKey       string
	removeUser         string
	removeNodes        []string
)

// Bootstrap flags
var (
	bootstrapHost         string
	bootstrapUser         string
	bootstrapSSHKey       string
	bootstrapDryRun       bool
	bootstrapTailscaleSSH bool
)

// Logs flags
var (
	logsNode         string
	logsApp          string
	logsTail         int
	logsGrep         string
	logsTailscaleSSH bool
	logsSSHKey       string
	logsUser         string
)

// Dashboard flags
var (
	dashTailscaleSSH bool
	dashSSHKey       string
	dashUser         string
	dashRefresh      int
)

// UI flags
var (
	uiAddr           string
	uiDBPath         string
	uiGitHubClientID string
	uiOpenBrowser    bool
	uiTailscaleSSH   bool
	uiSSHKey         string
	uiUser           string
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

// Setup registers all commands and flags on the root command tree.
// Called from Execute before running the CLI.
func Setup() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to cluster.yml config file")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(releaseCmd)
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(uiCmd)
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(lockCmd)
	rootCmd.AddCommand(bootstrapCmd)
	rootCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(shadowCmd)

	shadowCmd.AddCommand(shadowDeployCmd)
	shadowCmd.AddCommand(shadowPromoteCmd)
	shadowCmd.AddCommand(shadowRemoveCmd)
	shadowCmd.AddCommand(shadowStatusCmd)

	lockCmd.AddCommand(lockBreakCmd)

	deployCmd.Flags().BoolVar(&deployAll, "all", false, "Deploy all discovered apps")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Show actions without executing")
	deployCmd.Flags().BoolVar(&deployTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	deployCmd.Flags().StringVar(&deploySSHKey, "ssh-key", "", "Path to SSH private key")
	deployCmd.Flags().StringVar(&deployUser, "user", "", "SSH user (defaults to current user)")
	deployCmd.Flags().StringVar(&deployReleaseID, "release", "", "Release ID to deploy, or latest")

	statusCmd.Flags().BoolVar(&deployTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	statusCmd.Flags().StringVar(&deploySSHKey, "ssh-key", "", "Path to SSH private key")
	statusCmd.Flags().StringVar(&deployUser, "user", "", "SSH user (defaults to current user)")

	rollbackCmd.Flags().BoolVar(&deployTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	rollbackCmd.Flags().StringVar(&deploySSHKey, "ssh-key", "", "Path to SSH private key")
	rollbackCmd.Flags().StringVar(&deployUser, "user", "", "SSH user (defaults to current user)")

	logsCmd.Flags().StringVar(&logsNode, "node", "", "Target node ID (required)")
	logsCmd.Flags().StringVar(&logsApp, "app", "", "Filter to containers matching this app name")
	logsCmd.Flags().IntVarP(&logsTail, "tail", "n", 100, "Number of recent lines per container")
	logsCmd.Flags().StringVar(&logsGrep, "grep", "", "Server-side grep filter")
	logsCmd.Flags().BoolVar(&logsTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	logsCmd.Flags().StringVar(&logsSSHKey, "ssh-key", "", "Path to SSH private key")
	logsCmd.Flags().StringVar(&logsUser, "user", "", "SSH user (defaults to current user)")
	logsCmd.MarkFlagRequired("node")

	dashboardCmd.Flags().BoolVar(&dashTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	dashboardCmd.Flags().StringVar(&dashSSHKey, "ssh-key", "", "Path to SSH private key")
	dashboardCmd.Flags().StringVar(&dashUser, "user", "", "SSH user (defaults to current user)")
	dashboardCmd.Flags().IntVar(&dashRefresh, "refresh", 30, "Auto-refresh interval in seconds")

	defaultUI := warpd.DefaultLocalUIConfig()
	uiCmd.Flags().StringVar(&uiAddr, "addr", defaultUI.HTTPAddr, "Local UI listen address")
	uiCmd.Flags().StringVar(&uiDBPath, "db-path", defaultUI.DBPath, "Local UI database path")
	uiCmd.Flags().StringVar(&uiGitHubClientID, "github-client-id", os.Getenv("WARPGATE_GITHUB_CLIENT_ID"), "GitHub App client ID")
	uiCmd.Flags().BoolVar(&uiOpenBrowser, "open", defaultUI.OpenBrowser, "Open the local UI in a browser")
	uiCmd.Flags().BoolVar(&uiTailscaleSSH, "tailscale-ssh", defaultUI.Deploy.TailscaleSSH, "Use Tailscale SSH for runtime operations")
	uiCmd.Flags().StringVar(&uiSSHKey, "ssh-key", defaultUI.Deploy.SSHKey, "Path to SSH private key for runtime operations")
	uiCmd.Flags().StringVar(&uiUser, "user", defaultUI.Deploy.User, "SSH user for runtime operations")

	removeCmd.Flags().BoolVar(&removeAll, "all", false, "Remove all discovered apps")
	removeCmd.Flags().BoolVar(&removeForce, "force", false, "Skip confirmation prompt")
	removeCmd.Flags().BoolVar(&removeTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	removeCmd.Flags().StringVar(&removeSSHKey, "ssh-key", "", "Path to SSH private key")
	removeCmd.Flags().StringVar(&removeUser, "user", "", "SSH user (defaults to current user)")
	removeCmd.Flags().StringSliceVar(&removeNodes, "nodes", nil, "Override target nodes (comma-separated)")

	lockBreakCmd.Flags().BoolVar(&deployTailscaleSSH, "tailscale-ssh", false, "Use Tailscale SSH")
	lockBreakCmd.Flags().StringVar(&deploySSHKey, "ssh-key", "", "Path to SSH private key")

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

var deployCmd = &cobra.Command{
	Use:   "deploy [app-name] [version]",
	Short: "Deploy an application to its target nodes",
	Long: `Deploy a single app or all apps at once.

Examples:
  warpgate deploy myapp
  warpgate deploy myapp v2.0.0
  warpgate deploy --all`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if deployAll {
			if deployReleaseID != "" {
				return fmt.Errorf("--release cannot be used with --all")
			}
			if len(args) > 0 {
				return fmt.Errorf("--all does not accept app-name arguments")
			}
			if len(repo.Apps) == 0 {
				return fmt.Errorf("no apps found")
			}
			if !deployDryRun {
				fmt.Printf("Will deploy %d app(s):\n", len(repo.Apps))
				for _, app := range repo.Apps {
					fmt.Printf("  - %s (%d service(s))\n", app.Name, len(app.Release.Services))
				}
				fmt.Print("Continue? [y/N] ")
				var answer string
				fmt.Scanln(&answer)
				if answer != "y" && answer != "Y" {
					fmt.Println("Aborted.")
					return nil
				}
			}
			d := deploy.NewDeployer(repo, deploySSHKey)
			d.TailscaleSSH = deployTailscaleSSH
			d.DryRun = deployDryRun
			d.User = deployUser
			d.GitHubToken = os.Getenv("GITHUB_TOKEN")
			return d.DeployAll()
		}

		if len(args) == 0 {
			return fmt.Errorf("requires app-name argument or --all flag")
		}

		appName := args[0]
		version := ""
		if len(args) > 1 {
			version = args[1]
		}

		d := deploy.NewDeployer(repo, deploySSHKey)
		d.TailscaleSSH = deployTailscaleSSH
		d.DryRun = deployDryRun
		d.User = deployUser
		d.GitHubToken = os.Getenv("GITHUB_TOKEN")

		if deployReleaseID != "" {
			return d.DeployRelease(appName, deployReleaseID)
		}
		return d.Deploy(appName, version)
	},
}

var releaseCmd = &cobra.Command{
	Use:   "release <app-name>",
	Short: "Create a release manifest for an application",
	Long: `Create a release manifest by snapshotting the app image reference,
compose revision, and environment layer. The manifest is written under
apps/<app-name>/releases/ and latest.json is updated to the same release.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, "")
		d.GitHubToken = os.Getenv("GITHUB_TOKEN")

		manifest, err := d.CreateRelease(args[0])
		if err != nil {
			return err
		}

		fmt.Printf("Release: %s\n", manifest.ID)
		fmt.Printf("Compose: %s\n", manifest.ComposeRev)
		fmt.Printf("Env: %s\n", manifest.EnvHash)
		serviceNames := make([]string, 0, len(manifest.Services))
		for serviceName := range manifest.Services {
			serviceNames = append(serviceNames, serviceName)
		}
		sort.Strings(serviceNames)
		fmt.Println("Services:")
		for _, serviceName := range serviceNames {
			service := manifest.Services[serviceName]
			fmt.Printf("  - %s: %s (env: %s)\n", serviceName, service.ImageRef, service.EnvHash)
		}
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status [app-name]",
	Short: "Show cluster and application status",
	Long: `Show cluster overview or live deployment status for a specific app.

Without an app name, shows static cluster configuration.
With an app name, SSHes to target nodes and queries live container state.

Examples:
  warpgate status
  warpgate status myapp --tailscale-ssh`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return showClusterStatus()
		}

		d := deploy.NewDeployer(repo, deploySSHKey)
		d.TailscaleSSH = deployTailscaleSSH
		d.User = deployUser

		statuses, err := d.Status(args[0])
		if err != nil {
			return err
		}

		fmt.Printf("App: %s\n\n", args[0])
		for _, s := range statuses {
			if s.Error != "" {
				fmt.Printf("  %s: error — %s\n", s.NodeID, s.Error)
				continue
			}
			version := s.Version
			if version == "" {
				version = "none"
			}
			slot := s.Slot
			if slot == "" {
				slot = "-"
			}
			fmt.Printf("  %s: %s (version: %s, slot: %s)\n", s.NodeID, s.State, version, slot)
			if s.Containers != "" {
				for _, line := range strings.Split(s.Containers, "\n") {
					if line != "" {
						fmt.Printf("    %s\n", line)
					}
				}
			}
			if s.ShadowVersion != "" {
				fmt.Printf("    shadow: %s (version: %s)\n", s.ShadowState, s.ShadowVersion)
			}
		}

		return nil
	},
}

func showClusterStatus() error {
	fmt.Printf("Project: %s\n", repo.Cluster.Project)
	fmt.Printf("Nodes: %d\n\n", len(repo.Cluster.Nodes))

	for _, node := range repo.Cluster.Nodes {
		fmt.Printf("  %s (%s)", node.ID, node.Host)
		if node.PrivateIP != "" {
			fmt.Printf(" [private: %s]", node.PrivateIP)
		}
		fmt.Println()
	}

	fmt.Printf("\nApps: %d\n\n", len(repo.Apps))
	for _, app := range repo.Apps {
		fmt.Printf("  %s\n", app.Name)
		fmt.Printf("    Targets: %v\n", app.GetTargetNodes(repo.Cluster.Nodes))
		serviceNames := make([]string, 0, len(app.EffectiveReleaseServices()))
		for serviceName := range app.EffectiveReleaseServices() {
			serviceNames = append(serviceNames, serviceName)
		}
		sort.Strings(serviceNames)
		for _, serviceName := range serviceNames {
			service := app.EffectiveReleaseServices()[serviceName]
			fmt.Printf("    Service %s: %s\n", serviceName, service.EffectiveImageRef())
			expose := service.EffectiveExpose()
			if expose.Public != nil && len(expose.Public.Domains) > 0 {
				fmt.Printf("      Domains: %v\n", expose.Public.Domains)
			}
			if service.SecretsPrefix != "" {
				fmt.Printf("      Secrets: %s\n", service.SecretsPrefix)
			}
		}
	}

	return nil
}

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Live cluster status dashboard",
	Long: `Show a live, auto-refreshing dashboard of cluster and app status.

Connects to all nodes via SSH and displays node reachability, app versions,
blue/green slot state, and container health.

Examples:
  warpgate dashboard --tailscale-ssh
  warpgate dashboard --tailscale-ssh --refresh 10`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, dashSSHKey)
		d.TailscaleSSH = dashTailscaleSSH
		d.User = dashUser

		return tui.RunDashboard(tui.DashboardConfig{
			Project:         repo.Cluster.Project,
			Nodes:           repo.Cluster.Nodes,
			Apps:            repo.Apps,
			Fetch:           d.ClusterStatus,
			RefreshInterval: time.Duration(dashRefresh) * time.Second,
		})
	},
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the local Warpgate UI",
	Long: `Start a local browser UI for Warpgate.

The UI binds to loopback by default and uses GitHub App device flow for repository access.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := warpd.DefaultLocalUIConfig()
		cfg.HTTPAddr = uiAddr
		cfg.DBPath = uiDBPath
		cfg.GitHubClientID = uiGitHubClientID
		cfg.OpenBrowser = uiOpenBrowser
		cfg.Deploy.TailscaleSSH = uiTailscaleSSH
		cfg.Deploy.SSHKey = uiSSHKey
		cfg.Deploy.User = uiUser
		return warpd.RunLocalUI(context.Background(), cfg)
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show recent container logs from a node",
	Long: `Fetch recent Docker container logs from a specific node.
Shows logs from all running containers, or filter to a specific app.

Examples:
  warpgate logs --node node-1 --tailscale-ssh
  warpgate logs --node node-1 --app myapp --tailscale-ssh
  warpgate logs --node node-1 --tail 50 --grep "error" --tailscale-ssh`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, logsSSHKey)
		d.TailscaleSSH = logsTailscaleSSH
		d.User = logsUser
		return d.Logs(deploy.LogsOptions{
			NodeID: logsNode,
			App:    logsApp,
			Tail:   logsTail,
			Grep:   logsGrep,
		})
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback <app-name>",
	Short: "Rollback an application to the previous version",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, deploySSHKey)
		d.TailscaleSSH = deployTailscaleSSH
		d.User = deployUser
		return d.Rollback(args[0])
	},
}

var removeCmd = &cobra.Command{
	Use:   "remove [app-name]",
	Short: "Stop and remove an application from target nodes",
	Long: `Remove an application by stopping its containers and cleaning up
its files from all target nodes. If the app config has already been deleted
from apps/, all cluster nodes are scanned.

Examples:
  warpgate remove myapp --tailscale-ssh
  warpgate remove myapp --tailscale-ssh --force
  warpgate remove myapp --tailscale-ssh --nodes node-1,node-2
  warpgate remove --all --force`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if removeAll {
			if len(args) > 0 {
				return fmt.Errorf("--all does not accept app-name arguments")
			}
			if !removeForce {
				return fmt.Errorf("remove --all requires --force flag")
			}
			if len(repo.Apps) == 0 {
				return fmt.Errorf("no apps found")
			}
			fmt.Printf("Removing %d app(s):\n", len(repo.Apps))
			for _, app := range repo.Apps {
				fmt.Printf("  - %s\n", app.Name)
			}
			d := deploy.NewDeployer(repo, removeSSHKey)
			d.TailscaleSSH = removeTailscaleSSH
			d.User = removeUser
			return d.RemoveAll(removeNodes)
		}

		if len(args) == 0 {
			return fmt.Errorf("requires app-name argument or --all flag")
		}

		if !removeForce {
			fmt.Printf("This will stop and remove '%s' from target nodes.\n", args[0])
			fmt.Print("Continue? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		d := deploy.NewDeployer(repo, removeSSHKey)
		d.TailscaleSSH = removeTailscaleSSH
		d.User = removeUser
		return d.Remove(args[0], removeNodes)
	},
}

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Manage deploy locks",
}

var lockBreakCmd = &cobra.Command{
	Use:   "break <app-name>",
	Short: "Forcibly remove a stale deploy lock",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d := deploy.NewDeployer(repo, deploySSHKey)
		d.TailscaleSSH = deployTailscaleSSH
		d.User = deployUser
		return d.BreakLock(args[0])
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
