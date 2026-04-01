// Package config defines the warpgate.yml configuration types and loading.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ClusterConfig is the root configuration for Warpgate.
type ClusterConfig struct {
	// Version is the config schema version, defaults to "1".
	Version string `yaml:"version"`
	// Project is the project name, used as Docker Compose project prefix.
	Project string `yaml:"project"`
	// Nodes is the list of cluster nodes.
	Nodes []NodeConfig `yaml:"nodes"`
	// Networking holds Tailscale, DNS, and Traefik settings.
	Networking NetworkingConfig `yaml:"networking"`
	// Apps is the list of applications to deploy.
	Apps []AppConfig `yaml:"apps"`
	// Registry holds Docker registry credentials.
	Registry RegistryConfig `yaml:"registry"`
	// Secrets holds secrets provider configuration.
	Secrets SecretsConfig `yaml:"secrets"`
	// GoProxy is a private Go module proxy URL on the tailnet for installing private packages.
	GoProxy string `yaml:"go_proxy,omitempty"`
}

// NodeConfig defines a cluster node.
type NodeConfig struct {
	// ID is the unique node identifier.
	ID string `yaml:"id"`
	// Host is the IP address or hostname.
	Host string `yaml:"host"`
	// TailscaleIP is the node's Tailscale mesh IP.
	TailscaleIP string `yaml:"tailscale_ip,omitempty"`
	// Roles is the list of node roles: control-plane, worker. Defaults to both if empty.
	Roles []string `yaml:"roles,omitempty"`
	// Labels holds custom key-value labels for the node.
	Labels map[string]string `yaml:"labels,omitempty"`
}

// NetworkingConfig holds cluster networking settings.
type NetworkingConfig struct {
	// Tailnet is the Tailscale tailnet name.
	Tailnet string `yaml:"tailnet"`
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

// RegistryConfig holds Docker registry credentials.
type RegistryConfig struct {
	// Server is the registry hostname (e.g. "ghcr.io").
	Server string `yaml:"server"`
	// Username is the registry username.
	Username string `yaml:"username,omitempty"`
	// Password is the registry password or token.
	Password string `yaml:"password,omitempty"`
}

// SecretsConfig holds secrets provider configuration.
type SecretsConfig struct {
	// Provider is the secrets backend: secretsauce, env, or file.
	Provider string `yaml:"provider"`
	// Config holds provider-specific key-value settings.
	Config map[string]string `yaml:"config,omitempty"`
}

// AppConfig defines an application to deploy.
type AppConfig struct {
	// Name is the application name.
	Name string `yaml:"name"`
	// Image is the Docker image reference.
	Image string `yaml:"image"`
	// Version is the image tag, defaults to "latest".
	Version string `yaml:"version,omitempty"`
	// Replicas is the per-node replica count.
	Replicas int `yaml:"replicas,omitempty"`
	// Targets is the list of node IDs to deploy to, or ["all"] for all nodes.
	Targets []string `yaml:"targets,omitempty"`
	// Resources holds CPU and memory constraints.
	Resources ResourceConfig `yaml:"resources,omitempty"`
	// Volumes is the list of named volume mounts.
	Volumes []VolumeConfig `yaml:"volumes,omitempty"`
	// Ports is the list of port mappings.
	Ports []PortConfig `yaml:"ports,omitempty"`
	// Domains is the list of domain names for Traefik routing.
	Domains []string `yaml:"domains,omitempty"`
	// Env holds environment variables as key-value pairs.
	Env map[string]string `yaml:"env,omitempty"`
	// Secrets is the list of secret names to inject from the secrets provider.
	Secrets []string `yaml:"secrets,omitempty"`
	// HealthCheck holds health check configuration.
	HealthCheck HealthCheckConfig `yaml:"health_check,omitempty"`
	// Sidecars is the list of containers that run alongside the main app.
	Sidecars []SidecarConfig `yaml:"sidecars,omitempty"`
	// Init is the list of containers that run before the main app starts.
	Init []InitContainerConfig `yaml:"init,omitempty"`
	// ComposeFile is a path to a custom Docker Compose file override.
	ComposeFile string `yaml:"compose_file,omitempty"`
}

// SidecarConfig defines a container that runs alongside the main app.
type SidecarConfig struct {
	// Name is the sidecar name; the compose service becomes {app}-{name}.
	Name string `yaml:"name"`
	// Image is the Docker image reference.
	Image string `yaml:"image"`
	// Command overrides the container command.
	Command string `yaml:"command,omitempty"`
	// Volumes is the list of volume mounts in "name:/path" format.
	Volumes []string `yaml:"volumes,omitempty"`
	// Env holds environment variables.
	Env map[string]string `yaml:"env,omitempty"`
}

// InitContainerConfig defines a container that runs before the main app starts.
type InitContainerConfig struct {
	// Name is the init container name; the compose service becomes {app}-{name}.
	Name string `yaml:"name"`
	// Image is the Docker image reference.
	Image string `yaml:"image"`
	// Command is the command to run.
	Command string `yaml:"command,omitempty"`
	// Volumes is the list of volume mounts in "name:/path" format.
	Volumes []string `yaml:"volumes,omitempty"`
	// Env holds environment variables.
	Env map[string]string `yaml:"env,omitempty"`
}

// ResourceConfig holds CPU and memory constraints for an app.
type ResourceConfig struct {
	// CPUs is the CPU reservation (e.g. "0.5").
	CPUs string `yaml:"cpus,omitempty"`
	// Memory is the memory reservation (e.g. "512M").
	Memory string `yaml:"memory,omitempty"`
	// CPULimit is the CPU limit (e.g. "1.0").
	CPULimit string `yaml:"cpu_limit,omitempty"`
	// MemLimit is the memory limit (e.g. "1G").
	MemLimit string `yaml:"memory_limit,omitempty"`
}

// VolumeConfig defines a named volume mount.
type VolumeConfig struct {
	// Name is the named volume identifier.
	Name string `yaml:"name"`
	// Path is the mount path inside the container.
	Path string `yaml:"path"`
	// Size is an optional size hint.
	Size string `yaml:"size,omitempty"`
	// Backup indicates whether to include this volume in backups.
	Backup bool `yaml:"backup,omitempty"`
}

// PortConfig defines a port mapping.
type PortConfig struct {
	// Container is the container port number.
	Container int `yaml:"container"`
	// Host is the explicit host port, or 0 to expose only the container port.
	Host int `yaml:"host,omitempty"`
	// Protocol is the transport protocol: tcp (default) or udp.
	Protocol string `yaml:"protocol,omitempty"`
}

// HealthCheckConfig defines a health check for an app.
type HealthCheckConfig struct {
	// Path is the HTTP health check path (e.g. "/health").
	Path string `yaml:"path,omitempty"`
	// Port is the port for HTTP health checks.
	Port int `yaml:"port,omitempty"`
	// Command is a shell command health check, alternative to HTTP path.
	Command string `yaml:"command,omitempty"`
	// Interval is the check interval (e.g. "10s").
	Interval string `yaml:"interval,omitempty"`
	// Timeout is the check timeout (e.g. "5s").
	Timeout string `yaml:"timeout,omitempty"`
	// Retries is the failure count before marking unhealthy.
	Retries int `yaml:"retries,omitempty"`
}

// LoadClusterConfig reads and parses a warpgate.yml file with environment variable expansion.
func LoadClusterConfig(path string) (*ClusterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	expandedData := ExpandEnvVars(string(data))

	var config ClusterConfig
	if err := yaml.Unmarshal([]byte(expandedData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if config.Version == "" {
		config.Version = "1"
	}

	return &config, nil
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

// LoadClusterConfigForEnv loads config for a specific environment.
// It looks for warpgate.<env>.yml first, then falls back to the base path.
func LoadClusterConfigForEnv(basePath, env string) (*ClusterConfig, error) {
	envPath := strings.Replace(basePath, ".yml", fmt.Sprintf(".%s.yml", env), 1)

	if _, err := os.Stat(envPath); err == nil {
		return LoadClusterConfig(envPath)
	}

	return LoadClusterConfig(basePath)
}

// GetApp returns the app configuration with the given name, or nil if not found.
func (c *ClusterConfig) GetApp(name string) *AppConfig {
	for i := range c.Apps {
		if c.Apps[i].Name == name {
			return &c.Apps[i]
		}
	}
	return nil
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

// IsControlPlane reports whether this node has the control-plane role.
// Defaults to true if no roles are specified.
func (n *NodeConfig) IsControlPlane() bool {
	if len(n.Roles) == 0 {
		return true
	}
	for _, role := range n.Roles {
		if role == "control-plane" {
			return true
		}
	}
	return false
}

// GetTargetNodes returns the node IDs that should run this app.
// Returns all node IDs if targets is empty or ["all"].
func (a *AppConfig) GetTargetNodes(allNodes []NodeConfig) []string {
	if len(a.Targets) == 0 || (len(a.Targets) == 1 && a.Targets[0] == "all") {
		var nodes []string
		for _, node := range allNodes {
			nodes = append(nodes, node.ID)
		}
		return nodes
	}
	return a.Targets
}

// GetAppsForNode returns all apps that target the given node ID.
func (c *ClusterConfig) GetAppsForNode(nodeID string) []*AppConfig {
	var apps []*AppConfig
	for i := range c.Apps {
		for _, t := range c.Apps[i].GetTargetNodes(c.Nodes) {
			if t == nodeID {
				apps = append(apps, &c.Apps[i])
				break
			}
		}
	}
	return apps
}

// Validate checks the configuration for required fields and returns an error if invalid.
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

	for i, app := range c.Apps {
		if app.Name == "" {
			return fmt.Errorf("app %d: name is required", i)
		}
		if app.Image == "" {
			return fmt.Errorf("app %s: image is required", app.Name)
		}
	}

	return nil
}
