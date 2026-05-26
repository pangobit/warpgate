// Package warpd composes and runs the Warpgate web control plane.
package warpd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config holds daemon boot configuration.
type Config struct {
	// Mode selects server or agent behavior.
	Mode string
	// HTTPAddr is the web server listen address.
	HTTPAddr string
	// DBPath is the embedded Turso database path.
	DBPath string
	// LocalDev enables static local identity instead of Tailscale identity.
	LocalDev bool
	// AuthMode selects none, static, or tailscale web authentication.
	AuthMode string
	// LocalEmail is the local development user email.
	LocalEmail string
	// Tailscale holds Tailscale boot settings.
	Tailscale TailscaleConfig
	// Deploy holds deploy adapter settings.
	Deploy DeployConfig
}

// TailscaleConfig holds Tailscale web identity settings.
type TailscaleConfig struct {
	// Enabled enables Tailscale identity.
	Enabled bool
	// Hostname is the Tailscale hostname for the daemon.
	Hostname string
	// StateDir is the tsnet state directory.
	StateDir string
	// Tags are requested Tailscale tags.
	Tags string
	// OAuthClientID is the Tailscale OAuth client ID.
	OAuthClientID string
	// OAuthClientSecret is the Tailscale OAuth client secret.
	OAuthClientSecret string
}

// DeployConfig holds deploy adapter boot settings.
type DeployConfig struct {
	// RepoPath is a local checkout used by the legacy deploy adapter.
	RepoPath string
	// SSHKey is the deploy SSH private key path.
	SSHKey string
	// TailscaleSSH enables Tailscale SSH for deploys.
	TailscaleSSH bool
	// User is the deploy SSH user.
	User string
	// GitHubTokenEnvVar names the legacy GitHub token env var.
	GitHubTokenEnvVar string
}

// LocalUIConfig holds local browser UI configuration.
type LocalUIConfig struct {
	// HTTPAddr is the local web server listen address.
	HTTPAddr string
	// DBPath is the local UI database path.
	DBPath string
	// GitHubClientID is the GitHub App client ID used for device flow.
	GitHubClientID string
	// OpenBrowser opens the local UI in the default browser.
	OpenBrowser bool
	// Deploy holds deploy adapter settings.
	Deploy DeployConfig
}

// DefaultLocalUIConfig returns local browser UI defaults.
func DefaultLocalUIConfig() LocalUIConfig {
	return LocalUIConfig{
		HTTPAddr:    "127.0.0.1:0",
		DBPath:      defaultLocalDBPath(),
		OpenBrowser: true,
		Deploy: DeployConfig{
			RepoPath:          ".",
			TailscaleSSH:      true,
			GitHubTokenEnvVar: "GITHUB_TOKEN",
		},
	}
}

func defaultLocalDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "warpgate.db"
	}
	return filepath.Join(dir, "warpgate", "warpgate.db")
}

// LoadConfig loads daemon config from defaults, file, environment, and flags.
func LoadConfig(args []string) (Config, error) {
	flags := pflag.NewFlagSet("warpd", pflag.ContinueOnError)
	flags.String("config", "", "Path to warpd config file")
	flags.String("mode", "server", "Daemon mode")
	flags.String("http_addr", ":8080", "HTTP listen address")
	flags.String("db_path", "warpgate.db", "Embedded Turso database path")
	flags.String("auth_mode", "none", "Web auth mode: none, static, or tailscale")
	flags.Bool("local_dev", false, "Use static local identity")
	flags.String("local_email", "admin@example.com", "Local development identity email")
	flags.String("deploy.repo_path", "/opt/warpgate/infra", "Local infra repo checkout for deploys")
	flags.Bool("deploy.tailscale_ssh", true, "Use Tailscale SSH for deploys")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}

	v := viper.New()
	v.SetEnvPrefix("WARPGATE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	if err := v.BindPFlags(flags); err != nil {
		return Config{}, err
	}
	setDefaults(v)
	configPath := v.GetString("config")
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, err
		}
	}
	cfg := Config{
		Mode:       v.GetString("mode"),
		HTTPAddr:   v.GetString("http_addr"),
		DBPath:     v.GetString("db_path"),
		LocalDev:   v.GetBool("local_dev"),
		AuthMode:   v.GetString("auth_mode"),
		LocalEmail: v.GetString("local_email"),
		Tailscale: TailscaleConfig{
			Enabled:           v.GetBool("tailscale.enabled"),
			Hostname:          v.GetString("tailscale.hostname"),
			StateDir:          v.GetString("tailscale.state_dir"),
			Tags:              v.GetString("tailscale.tags"),
			OAuthClientID:     v.GetString("tailscale.oauth_client_id"),
			OAuthClientSecret: v.GetString("tailscale.oauth_client_secret"),
		},
		Deploy: DeployConfig{
			RepoPath:          v.GetString("deploy.repo_path"),
			SSHKey:            v.GetString("deploy.ssh_key"),
			TailscaleSSH:      v.GetBool("deploy.tailscale_ssh"),
			User:              v.GetString("deploy.user"),
			GitHubTokenEnvVar: v.GetString("deploy.github_token_env_var"),
		},
	}
	return cfg, cfg.Validate()
}

// Validate checks daemon boot configuration.
func (c Config) Validate() error {
	switch c.Mode {
	case "server", "agent":
	default:
		return fmt.Errorf("mode must be server or agent")
	}
	if c.HTTPAddr == "" {
		return fmt.Errorf("http_addr is required")
	}
	if c.DBPath == "" {
		return fmt.Errorf("db_path is required")
	}
	switch c.AuthMode {
	case "none", "static", "tailscale":
	default:
		return fmt.Errorf("auth_mode must be none, static, or tailscale")
	}
	if c.LocalDev && c.LocalEmail == "" {
		return fmt.Errorf("local_email is required in local_dev mode")
	}
	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("mode", "server")
	v.SetDefault("http_addr", ":8080")
	v.SetDefault("db_path", "warpgate.db")
	v.SetDefault("auth_mode", "none")
	v.SetDefault("local_dev", false)
	v.SetDefault("local_email", "admin@example.com")
	v.SetDefault("tailscale.enabled", true)
	v.SetDefault("tailscale.hostname", "warpgate")
	v.SetDefault("tailscale.state_dir", "/var/lib/warpgate/tsnet")
	v.SetDefault("tailscale.tags", "tag:warpgate")
	v.SetDefault("deploy.repo_path", "/opt/warpgate/infra")
	v.SetDefault("deploy.tailscale_ssh", true)
	v.SetDefault("deploy.github_token_env_var", "GITHUB_TOKEN")
	v.SetDefault("poll.config_interval", 5*time.Minute)
	v.SetDefault("poll.images_interval", 15*time.Minute)
}
