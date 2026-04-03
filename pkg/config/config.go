// Package config defines the warpgate configuration types and loading.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ClusterConfig is the cluster-level configuration loaded from cluster.yml.
type ClusterConfig struct {
	// Version is the config schema version.
	Version string `yaml:"version"`
	// Project is the project name, used as Docker Compose project prefix.
	Project string `yaml:"project"`
	// Nodes is the list of cluster nodes.
	Nodes []NodeConfig `yaml:"nodes"`
	// Networking holds Tailscale, DNS, and Traefik settings.
	Networking NetworkingConfig `yaml:"networking"`
	// Registry holds Docker registry credentials.
	Registry RegistryConfig `yaml:"registry"`
	// Secrets holds secrets management settings.
	Secrets SecretsConfig `yaml:"secrets,omitempty"`
	// GoProxy is a private Go module proxy URL for installing private packages.
	GoProxy string `yaml:"go_proxy,omitempty"`
}

// NodeConfig defines a cluster node.
type NodeConfig struct {
	// ID is the unique node identifier.
	ID string `yaml:"id"`
	// Host is the IP address or hostname.
	Host string `yaml:"host"`
	// PrivateIP is the node's private network IP (e.g. Tailscale).
	PrivateIP string `yaml:"private_ip,omitempty"`
}

// NetworkingConfig holds cluster networking settings.
type NetworkingConfig struct {
	// PrivateNetwork is the private network name (e.g. Tailscale tailnet).
	PrivateNetwork string `yaml:"private_network"`
	// DNS holds DNS provider settings.
	DNS DNSConfig `yaml:"dns"`
	// Traefik holds reverse proxy settings.
	Traefik TraefikConfig `yaml:"traefik"`
}

// DNSConfig holds DNS provider settings.
type DNSConfig struct {
	// Provider is the DNS provider name (e.g. "cloudflare").
	Provider string `yaml:"provider"`
	// Zone is the DNS zone (e.g. "example.com").
	Zone string `yaml:"zone"`
	// APIToken is the DNS provider API token.
	APIToken string `yaml:"api_token,omitempty"`
}

// TraefikConfig holds Traefik reverse proxy settings.
type TraefikConfig struct {
	// EntryPoints is the list of Traefik entrypoints (e.g. web, websecure).
	EntryPoints []string `yaml:"entry_points"`
	// ACME holds automatic HTTPS certificate settings.
	ACME ACMEConfig `yaml:"acme,omitempty"`
}

// ACMEConfig holds Let's Encrypt / ZeroSSL certificate settings.
type ACMEConfig struct {
	// Enabled enables automatic HTTPS via ACME.
	Enabled bool `yaml:"enabled"`
	// Email is the ACME registration email.
	Email string `yaml:"email"`
	// Provider is the ACME provider (letsencrypt or zerossl).
	Provider string `yaml:"provider"`
	// Staging uses the staging ACME endpoint to avoid rate limits.
	Staging bool `yaml:"staging,omitempty"`
}

// SecretsConfig holds secrets management settings.
type SecretsConfig struct {
	// Server is the SecretSauce server URL on the private network.
	Server string `yaml:"server,omitempty"`
}

// RegistryConfig holds Docker registry credentials.
type RegistryConfig struct {
	// Server is the registry hostname (e.g. "ghcr.io").
	Server string `yaml:"server"`
	// Username is the registry username.
	Username string `yaml:"username,omitempty"`
	// Password is the registry password or token.
	Password string `yaml:"password,omitempty"`
}

// ExposeConfig declares how a service is reachable at each visibility tier.
type ExposeConfig struct {
	// Public makes the service reachable from the internet via the public Traefik proxy.
	Public *PublicExpose `yaml:"public,omitempty"`
	// Private makes the service reachable on the node's private network IP via the internal Traefik proxy.
	Private *PrivateExpose `yaml:"private,omitempty"`
	// Internal enables cross-node routing via the internal Traefik proxy file provider.
	Internal *InternalExpose `yaml:"internal,omitempty"`
}

// PublicExpose configures internet-facing routing via the public Traefik proxy.
type PublicExpose struct {
	// Domains is the list of domain names for Traefik Host() routing.
	Domains []string `yaml:"domains"`
}

// PrivateExpose configures a port on the internal Traefik proxy (bound to private IP).
type PrivateExpose struct {
	// Port is the port the internal Traefik proxy listens on for this service.
	Port int `yaml:"port"`
}

// InternalExpose enables cross-node service-to-service routing via hostname.
type InternalExpose struct {
	// Hostname is the internal hostname (e.g., "auth.internal").
	Hostname string `yaml:"hostname"`
}

// SidecarConfig defines a sidecar service that needs Traefik routing.
type SidecarConfig struct {
	// Port is the container port the sidecar listens on.
	Port int `yaml:"port"`
	// Expose declares how the sidecar is reachable.
	Expose *ExposeConfig `yaml:"expose,omitempty"`
}

// EffectiveExpose returns the sidecar's expose config, or a zero value if nil.
func (s *SidecarConfig) EffectiveExpose() ExposeConfig {
	if s.Expose != nil {
		return *s.Expose
	}
	return ExposeConfig{}
}

// AppConfig defines an application's deployment metadata, loaded from app.yml.
type AppConfig struct {
	// Name is derived from the directory name, not from YAML.
	Name string `yaml:"-"`
	// Image is the Docker image reference (without tag).
	Image string `yaml:"image"`
	// Version is the image tag, defaults to "latest".
	Version string `yaml:"version,omitempty"`
	// Targets is the list of node IDs to deploy to. Empty means all nodes.
	Targets []string `yaml:"targets,omitempty"`
	// SecretsPrefix is the secretsauce prefix for secret injection.
	SecretsPrefix string `yaml:"secrets_prefix,omitempty"`
	// Port is the container port Traefik routes to.
	Port int `yaml:"port,omitempty"`
	// Expose declares how the app is reachable at each visibility tier.
	Expose *ExposeConfig `yaml:"expose,omitempty"`
	// Sidecars maps compose service names to their routing configuration.
	Sidecars map[string]SidecarConfig `yaml:"sidecars,omitempty"`
}

// EffectiveExpose returns the app's expose config, or a zero value if nil.
func (a *AppConfig) EffectiveExpose() ExposeConfig {
	if a.Expose != nil {
		return *a.Expose
	}
	return ExposeConfig{}
}

// LoadClusterConfig reads and parses a cluster.yml file with environment variable expansion.
func LoadClusterConfig(path string) (*ClusterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	expandedData := ExpandEnvVars(string(data))

	var cfg ClusterConfig
	if err := yaml.Unmarshal([]byte(expandedData), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.Version == "" {
		cfg.Version = "1"
	}

	return &cfg, nil
}

// LoadAppConfig reads and parses an app.yml file from the given directory.
// The app name is derived from the directory name.
func LoadAppConfig(dir string) (*AppConfig, error) {
	path := dir + "/app.yml"
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read app config: %w", err)
	}

	expandedData := ExpandEnvVars(string(data))

	var app AppConfig
	if err := yaml.Unmarshal([]byte(expandedData), &app); err != nil {
		return nil, fmt.Errorf("failed to parse app config %s: %w", path, err)
	}

	return &app, nil
}

// ExpandEnvVars expands ${VAR} and ${VAR:-default} syntax in the given string.
func ExpandEnvVars(input string) string {
	return os.Expand(input, func(key string) string {
		if idx := strings.Index(key, ":-"); idx > 0 {
			varName := key[:idx]
			defaultVal := key[idx+2:]
			if val := os.Getenv(varName); val != "" {
				return val
			}
			return defaultVal
		}
		return os.Getenv(key)
	})
}

// GetNode returns the node configuration with the given ID, or nil if not found.
func (c *ClusterConfig) GetNode(id string) *NodeConfig {
	for i := range c.Nodes {
		if c.Nodes[i].ID == id {
			return &c.Nodes[i]
		}
	}
	return nil
}

// GetTargetNodes returns the node IDs that should run this app.
// Returns all node IDs if targets is empty.
func (a *AppConfig) GetTargetNodes(allNodes []NodeConfig) []string {
	if len(a.Targets) == 0 {
		var nodes []string
		for _, node := range allNodes {
			nodes = append(nodes, node.ID)
		}
		return nodes
	}
	return a.Targets
}

// Validate checks the cluster configuration for required fields.
func (c *ClusterConfig) Validate() error {
	if c.Project == "" {
		return fmt.Errorf("project name is required")
	}

	if len(c.Nodes) == 0 {
		return fmt.Errorf("at least one node is required")
	}

	for i, node := range c.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node %d: id is required", i)
		}
		if node.Host == "" {
			return fmt.Errorf("node %d: host is required", i)
		}
	}

	return nil
}

// ValidateApp checks an app configuration for required fields.
func ValidateApp(app *AppConfig) error {
	if app.Name == "" {
		return fmt.Errorf("app name is required")
	}
	if app.Image == "" {
		return fmt.Errorf("app %s: image is required", app.Name)
	}

	expose := app.EffectiveExpose()
	if expose.Public != nil {
		if len(expose.Public.Domains) == 0 {
			return fmt.Errorf("app %s: expose.public requires at least one domain", app.Name)
		}
		if app.Port == 0 {
			return fmt.Errorf("app %s: expose.public requires port to be set", app.Name)
		}
	}
	if expose.Private != nil {
		if expose.Private.Port <= 0 {
			return fmt.Errorf("app %s: expose.private requires a port", app.Name)
		}
	}
	if expose.Internal != nil {
		if expose.Internal.Hostname == "" {
			return fmt.Errorf("app %s: expose.internal requires a hostname", app.Name)
		}
		if app.Port == 0 {
			return fmt.Errorf("app %s: expose.internal requires port to be set", app.Name)
		}
	}

	return nil
}
