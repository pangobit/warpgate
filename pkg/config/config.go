// Package config defines the warpgate configuration types and loading.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// validAppName matches DNS-safe names: lowercase alphanumeric and hyphens, not starting/ending with a hyphen.
var validAppName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// validVolumeName matches conservative Docker-compatible volume names.
var validVolumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

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
	// Challenge is the ACME challenge type (tls or dns).
	Challenge string `yaml:"challenge,omitempty"`
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

// ReleaseConfig declares the services that participate in a Warpgate release.
type ReleaseConfig struct {
	// Services maps Docker Compose service names to release-owned inputs.
	Services map[string]ReleaseServiceConfig `yaml:"services,omitempty"`
}

// ReleaseServiceConfig declares release-owned inputs for one Compose service.
type ReleaseServiceConfig struct {
	// Image is the Docker image reference without tag or digest.
	Image string `yaml:"image"`
	// ImageTag is the Docker image tag used when ImageDigest is empty.
	ImageTag string `yaml:"image_tag,omitempty"`
	// ImageDigest is the immutable Docker image digest for release creation.
	ImageDigest string `yaml:"image_digest,omitempty"`
	// SecretsPrefix is the secretsauce prefix for this service.
	SecretsPrefix string `yaml:"secrets_prefix,omitempty"`
	// Port is the container port used for service-level routing metadata.
	Port int `yaml:"port,omitempty"`
	// Expose declares how the service is reachable at each visibility tier.
	Expose *ExposeConfig `yaml:"expose,omitempty"`
	// Environment provides non-secret environment variables for this service.
	Environment map[string]string `yaml:"environment,omitempty"`
}

// EffectiveImageTag returns the configured image tag.
func (s ReleaseServiceConfig) EffectiveImageTag() string {
	if s.ImageTag != "" {
		return s.ImageTag
	}
	return "latest"
}

// EffectiveImageRef returns an immutable digest reference when configured, otherwise a tag reference.
func (s ReleaseServiceConfig) EffectiveImageRef() string {
	if s.ImageDigest != "" {
		return s.Image + "@" + s.ImageDigest
	}
	return s.Image + ":" + s.EffectiveImageTag()
}

// EffectiveExpose returns the service's expose config, or a zero value if nil.
func (s ReleaseServiceConfig) EffectiveExpose() ExposeConfig {
	if s.Expose != nil {
		return *s.Expose
	}
	return ExposeConfig{}
}

// EffectiveExpose returns the sidecar's expose config, or a zero value if nil.
func (s *SidecarConfig) EffectiveExpose() ExposeConfig {
	if s.Expose != nil {
		return *s.Expose
	}
	return ExposeConfig{}
}

// DeployStrategy controls how blue/green slot transitions are performed.
type DeployStrategy string

const (
	// StrategyBlueGreen is the default zero-downtime strategy: start new, health check, stop old.
	StrategyBlueGreen DeployStrategy = "blue-green"
	// StrategyRecreate stops the old slot before starting the new one.
	// Use this for apps with host port bindings that prevent two slots from running simultaneously.
	StrategyRecreate DeployStrategy = "recreate"
)

// SourceConfig defines a remote source for the compose file.
type SourceConfig struct {
	// Repo is the GitHub repository (e.g., "github.com/owner/repo").
	Repo string `yaml:"repo"`
	// ComposePath is the path to the compose file within the repo (default: "compose.yml").
	ComposePath string `yaml:"compose_path,omitempty"`
}

// PersistentVolumeConfig remaps a compose volume key to a stable Docker volume name.
type PersistentVolumeConfig struct {
	// ComposeName is the volume key declared in the source compose file.
	ComposeName string `yaml:"compose_name"`
	// Name is the actual Docker volume name to use on the target node.
	Name string `yaml:"name"`
}

// AppConfig defines an application's deployment metadata, loaded from app.yml.
type AppConfig struct {
	// Kind identifies the config type, expected to be "warpgate/app".
	Kind string `yaml:"kind,omitempty"`
	// Name is derived from the directory name, not from YAML.
	Name string `yaml:"-"`
	// Image is no longer used; declare release.services.<name>.image instead.
	Image string `yaml:"image"`
	// Version is no longer used; declare compose_ref or release service image tags instead.
	Version string `yaml:"version,omitempty"`
	// ImageTag is no longer used; declare release.services.<name>.image_tag instead.
	ImageTag string `yaml:"image_tag,omitempty"`
	// ImageDigest is no longer used; declare release.services.<name>.image_digest instead.
	ImageDigest string `yaml:"image_digest,omitempty"`
	// ComposeRef is the source reference for the compose file when Source is set.
	ComposeRef string `yaml:"compose_ref,omitempty"`
	// Targets is the list of node IDs to deploy to. Empty means all nodes.
	Targets []string `yaml:"targets,omitempty"`
	// SecretsPrefix is no longer used; declare release.services.<name>.secrets_prefix instead.
	SecretsPrefix string `yaml:"secrets_prefix,omitempty"`
	// Port is no longer used; declare release.services.<name>.port instead.
	Port int `yaml:"port,omitempty"`
	// Strategy is the deploy strategy: "blue-green" (default) or "recreate".
	Strategy DeployStrategy `yaml:"strategy,omitempty"`
	// Expose is no longer used; declare release.services.<name>.expose instead.
	Expose *ExposeConfig `yaml:"expose,omitempty"`
	// Sidecars is no longer used; declare each service under release.services instead.
	Sidecars map[string]SidecarConfig `yaml:"sidecars,omitempty"`
	// Release declares first-class services that are bundled into one release.
	Release ReleaseConfig `yaml:"release,omitempty"`
	// Source specifies a remote GitHub repo to fetch compose from. If set, compose.yml
	// is not expected locally and will be fetched at deploy time.
	Source *SourceConfig `yaml:"source,omitempty"`
	// PersistentVolumes remaps compose volume keys to stable Docker volume names.
	PersistentVolumes []PersistentVolumeConfig `yaml:"persistent_volumes,omitempty"`
	// Environment is no longer used; declare release.services.<name>.environment instead.
	Environment map[string]string `yaml:"environment,omitempty"`
}

// EffectiveReleaseServices returns the first-class services that make up a release.
func (a *AppConfig) EffectiveReleaseServices() map[string]ReleaseServiceConfig {
	services := make(map[string]ReleaseServiceConfig, len(a.Release.Services))
	for name, service := range a.Release.Services {
		services[name] = service
	}
	return services
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
		if node.PrivateIP != "" {
			if _, err := netip.ParseAddr(node.PrivateIP); err != nil {
				return fmt.Errorf("node %s: private_ip must be an IP address: %s", node.ID, node.PrivateIP)
			}
		}
	}

	return nil
}

// ValidateApp checks an app configuration for required fields.
func ValidateApp(app *AppConfig) error {
	if app.Name == "" {
		return fmt.Errorf("app name is required")
	}
	if !validAppName.MatchString(app.Name) {
		return fmt.Errorf("app name %q is invalid: must be lowercase alphanumeric and hyphens only", app.Name)
	}
	if app.Image != "" || app.Version != "" || app.ImageTag != "" || app.ImageDigest != "" || app.SecretsPrefix != "" || app.Port != 0 || app.Expose != nil || len(app.Environment) > 0 || len(app.Sidecars) > 0 {
		return fmt.Errorf("app %s: top-level image, version, image_tag, image_digest, secrets_prefix, port, expose, environment, and sidecars are no longer supported; use release.services", app.Name)
	}

	if len(app.Release.Services) == 0 {
		return fmt.Errorf("app %s: release.services is required", app.Name)
	}

	if app.Source != nil && app.ComposeRef == "" {
		return fmt.Errorf("app %s: compose_ref is required when source is set", app.Name)
	}

	switch app.Strategy {
	case "", StrategyBlueGreen, StrategyRecreate:
		// valid
	default:
		return fmt.Errorf("app %s: invalid strategy %q (must be \"blue-green\" or \"recreate\")", app.Name, app.Strategy)
	}

	for serviceName, service := range app.Release.Services {
		if !validVolumeName.MatchString(serviceName) {
			return fmt.Errorf("app %s: release.services %q is invalid", app.Name, serviceName)
		}
		if service.Image == "" {
			return fmt.Errorf("app %s: release.services.%s.image is required", app.Name, serviceName)
		}
		expose := service.EffectiveExpose()
		if expose.Public != nil {
			if len(expose.Public.Domains) == 0 {
				return fmt.Errorf("app %s: release.services.%s.expose.public requires at least one domain", app.Name, serviceName)
			}
			if service.Port == 0 {
				return fmt.Errorf("app %s: release.services.%s.expose.public requires port to be set", app.Name, serviceName)
			}
		}
		if expose.Private != nil && expose.Private.Port <= 0 {
			return fmt.Errorf("app %s: release.services.%s.expose.private requires a port", app.Name, serviceName)
		}
		if expose.Internal != nil {
			if expose.Internal.Hostname == "" {
				return fmt.Errorf("app %s: release.services.%s.expose.internal requires hostname", app.Name, serviceName)
			}
			if service.Port == 0 {
				return fmt.Errorf("app %s: release.services.%s.expose.internal requires port to be set", app.Name, serviceName)
			}
		}
	}

	seenComposeNames := make(map[string]struct{}, len(app.PersistentVolumes))
	seenVolumeNames := make(map[string]struct{}, len(app.PersistentVolumes))
	for _, volume := range app.PersistentVolumes {
		if volume.ComposeName == "" {
			return fmt.Errorf("app %s: persistent_volumes.compose_name is required", app.Name)
		}
		if volume.Name == "" {
			return fmt.Errorf("app %s: persistent_volumes.name is required", app.Name)
		}
		if !validVolumeName.MatchString(volume.ComposeName) {
			return fmt.Errorf("app %s: persistent_volumes.compose_name %q is invalid", app.Name, volume.ComposeName)
		}
		if !validVolumeName.MatchString(volume.Name) {
			return fmt.Errorf("app %s: persistent_volumes.name %q is invalid", app.Name, volume.Name)
		}
		if _, exists := seenComposeNames[volume.ComposeName]; exists {
			return fmt.Errorf("app %s: duplicate persistent_volumes.compose_name %q", app.Name, volume.ComposeName)
		}
		if _, exists := seenVolumeNames[volume.Name]; exists {
			return fmt.Errorf("app %s: duplicate persistent_volumes.name %q", app.Name, volume.Name)
		}
		seenComposeNames[volume.ComposeName] = struct{}{}
		seenVolumeNames[volume.Name] = struct{}{}
	}

	return nil
}
