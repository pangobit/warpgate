// Package compose generates Docker Compose override and Traefik bootstrap files.
package compose

import (
	"fmt"
	"strings"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

// OverrideFile represents a docker-compose.override.yml structure.
type OverrideFile struct {
	// Services maps service names to their override definitions.
	Services map[string]ServiceOverride `yaml:"services"`
	// Volumes remaps compose volume keys to stable Docker volume names.
	Volumes map[string]VolumeOverride `yaml:"volumes,omitempty"`
}

// ServiceOverride holds the fields injected by warpgate into a compose override.
type ServiceOverride struct {
	// Image is the full image reference with tag.
	Image string `yaml:"image,omitempty"`
	// ExtraHosts maps internal hostnames to the node's private IP for service discovery.
	ExtraHosts []string `yaml:"extra_hosts,omitempty"`
	// Labels sets Docker container labels (used by shadow overrides to disable public Traefik).
	Labels map[string]string `yaml:"labels,omitempty"`
}

// VolumeOverride holds the fields injected into top-level compose volume definitions.
type VolumeOverride struct {
	// Name is the actual Docker volume name that Compose should use.
	Name string `yaml:"name,omitempty"`
}

// Network represents a Docker Compose network definition.
type Network struct {
	// External indicates the network is managed outside this compose file.
	External bool `yaml:"external,omitempty"`
}

func acmeChallenge(cfg config.ACMEConfig) string {
	if cfg.Challenge == "" {
		return "tls"
	}
	return strings.ToLower(cfg.Challenge)
}

// GenerateOverride creates a docker-compose.override.yml that injects the image tag
// and extra_hosts for internal service discovery. nodePrivateIP is the IP address
// containers use to resolve internal hostnames. Traefik labels and network
// configuration are authored directly in each app's compose.yml.
func GenerateOverride(app *config.AppConfig, networking *config.NetworkingConfig, internalHosts []string, nodePrivateIP string, composeContent string) (string, error) {
	version := app.Version
	if version == "" {
		version = "latest"
	}

	serviceNames, err := ParseComposeServices(composeContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse compose services: %w", err)
	}
	if len(serviceNames) == 0 {
		serviceNames = []string{app.Name}
	}

	var extraHosts []string
	for _, host := range internalHosts {
		extraHosts = append(extraHosts, host+":"+nodePrivateIP)
	}

	services := make(map[string]ServiceOverride)

	for _, svcName := range serviceNames {
		svc := ServiceOverride{
			ExtraHosts: extraHosts,
		}

		if svcName == app.Name {
			svc.Image = app.Image + ":" + version
		}

		services[svcName] = svc
	}

	override := &OverrideFile{
		Services: services,
	}
	if len(app.PersistentVolumes) > 0 {
		override.Volumes = make(map[string]VolumeOverride, len(app.PersistentVolumes))
		for _, volume := range app.PersistentVolumes {
			override.Volumes[volume.ComposeName] = VolumeOverride{Name: volume.Name}
		}
	}

	yamlBytes, err := yaml.Marshal(override)
	if err != nil {
		return "", fmt.Errorf("failed to marshal override: %w", err)
	}

	return string(yamlBytes), nil
}

// GenerateShadowOverride creates a docker-compose.shadow-override.yml that disables
// public Traefik routing via traefik.enable=false, sets the shadow image tag, and
// configures extra_hosts for internal service discovery. The shadow hostname
// (shadow-{app}.internal) is added to extra_hosts for direct access over Tailscale.
func GenerateShadowOverride(app *config.AppConfig, version string, internalHosts []string, nodePrivateIP string, composeContent string) (string, error) {
	if version == "" {
		version = "latest"
	}

	serviceNames, err := ParseComposeServices(composeContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse compose services: %w", err)
	}
	if len(serviceNames) == 0 {
		serviceNames = []string{app.Name}
	}

	var extraHosts []string
	for _, host := range internalHosts {
		extraHosts = append(extraHosts, host+":"+nodePrivateIP)
	}
	if ie := app.EffectiveExpose().Internal; ie != nil {
		extraHosts = append(extraHosts, "shadow-"+ie.Hostname+":"+nodePrivateIP)
	}

	labels := map[string]string{
		"traefik.enable": "false",
	}

	services := make(map[string]ServiceOverride)
	for _, svcName := range serviceNames {
		svc := ServiceOverride{
			ExtraHosts: extraHosts,
			Labels:     labels,
		}
		if svcName == app.Name {
			svc.Image = app.Image + ":" + version
		}
		services[svcName] = svc
	}

	override := &OverrideFile{
		Services: services,
	}

	yamlBytes, err := yaml.Marshal(override)
	if err != nil {
		return "", fmt.Errorf("failed to marshal shadow override: %w", err)
	}

	return string(yamlBytes), nil
}

// GenerateTraefikCompose creates the public Traefik service compose file for bootstrap.
func GenerateTraefikCompose(networking *config.NetworkingConfig) (string, error) {
	cmd := []string{
		"--providers.docker=true",
		"--providers.docker.network=warpgate",
		"--providers.docker.exposedbydefault=false",
	}

	portMap := map[string]int{
		"web":       80,
		"websecure": 443,
	}

	var ports []string
	for _, ep := range networking.Traefik.EntryPoints {
		port, ok := portMap[ep]
		if !ok {
			continue
		}
		cmd = append(cmd, fmt.Sprintf("--entrypoints.%s.address=:%d", ep, port))
		ports = append(ports, fmt.Sprintf("%d:%d", port, port))
	}

	if networking.Traefik.ACME.Enabled {
		resolver := networking.Traefik.ACME.Provider
		cmd = append(cmd,
			fmt.Sprintf("--certificatesresolvers.%s.acme.email=%s", resolver, networking.Traefik.ACME.Email),
			fmt.Sprintf("--certificatesresolvers.%s.acme.storage=/letsencrypt/acme.json", resolver),
		)
		switch acmeChallenge(networking.Traefik.ACME) {
		case "dns":
			cmd = append(cmd,
				fmt.Sprintf("--certificatesresolvers.%s.acme.dnschallenge=true", resolver),
				fmt.Sprintf("--certificatesresolvers.%s.acme.dnschallenge.provider=%s", resolver, networking.DNS.Provider),
				fmt.Sprintf("--certificatesresolvers.%s.acme.dnschallenge.resolvers=1.1.1.1:53,8.8.8.8:53", resolver),
			)
		default:
			cmd = append(cmd,
				fmt.Sprintf("--certificatesresolvers.%s.acme.tlschallenge=true", resolver),
			)
		}
		if networking.Traefik.ACME.Staging {
			cmd = append(cmd,
				fmt.Sprintf("--certificatesresolvers.%s.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory", resolver),
			)
		}
	}

	type traefikService struct {
		Image       string   `yaml:"image"`
		Restart     string   `yaml:"restart"`
		Command     []string `yaml:"command"`
		Ports       []string `yaml:"ports"`
		Volumes     []string `yaml:"volumes"`
		Networks    []string `yaml:"networks"`
		EnvFile     []string `yaml:"env_file,omitempty"`
		Environment []string `yaml:"environment,omitempty"`
	}

	type traefikCompose struct {
		Services map[string]traefikService `yaml:"services"`
		Networks map[string]Network        `yaml:"networks"`
		Volumes  map[string]struct{}       `yaml:"volumes"`
	}

	compose := traefikCompose{
		Services: map[string]traefikService{
			"traefik": {
				Image:   "traefik:v3.6",
				Restart: "unless-stopped",
				Command: cmd,
				Ports:   ports,
				Volumes: []string{
					"/var/run/docker.sock:/var/run/docker.sock:ro",
					"traefik-acme:/letsencrypt",
				},
				Networks: []string{"warpgate"},
				Environment: []string{
					"DOCKER_API_VERSION=1.45",
				},
			},
		},
		Networks: map[string]Network{
			"warpgate": {External: true},
		},
		Volumes: map[string]struct{}{
			"traefik-acme": {},
		},
	}
	if networking.Traefik.ACME.Enabled && acmeChallenge(networking.Traefik.ACME) == "dns" {
		service := compose.Services["traefik"]
		service.EnvFile = []string{"/etc/warpgate/traefik/acme.env"}
		compose.Services["traefik"] = service
	}

	yamlBytes, err := yaml.Marshal(compose)
	if err != nil {
		return "", fmt.Errorf("failed to marshal traefik compose: %w", err)
	}

	return string(yamlBytes), nil
}
