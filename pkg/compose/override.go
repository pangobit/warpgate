// Package compose generates Docker Compose override files for Traefik integration.
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
	// Networks maps network names to their definitions.
	Networks map[string]Network `yaml:"networks,omitempty"`
}

// ServiceOverride holds the fields injected by warpgate into a compose override.
type ServiceOverride struct {
	// Image is the full image reference with tag.
	Image string `yaml:"image,omitempty"`
	// Labels holds container labels (Traefik routing).
	Labels map[string]string `yaml:"labels,omitempty"`
	// Networks is the list of networks to attach to.
	Networks []string `yaml:"networks,omitempty"`
	// ExtraHosts maps internal hostnames to host-gateway for service discovery.
	ExtraHosts []string `yaml:"extra_hosts,omitempty"`
}

// Network represents a Docker Compose network definition.
type Network struct {
	// External indicates the network is managed outside this compose file.
	External bool `yaml:"external,omitempty"`
}

// GenerateOverride creates a docker-compose.override.yml that injects the image tag,
// Traefik labels, the warpgate network, and extra_hosts for internal service discovery.
// internalHosts is the list of all internal hostnames across the cluster.
func GenerateOverride(app *config.AppConfig, networking *config.NetworkingConfig, internalHosts []string) (string, error) {
	version := app.Version
	if version == "" {
		version = "latest"
	}

	labels := buildTraefikLabels(app, networking)
	buildInternalLabels(app, labels)

	var extraHosts []string
	for _, host := range internalHosts {
		extraHosts = append(extraHosts, host+":host-gateway")
	}

	svc := ServiceOverride{
		Image:      app.Image + ":" + version,
		Networks:   []string{"warpgate"},
		Labels:     labels,
		ExtraHosts: extraHosts,
	}

	override := &OverrideFile{
		Services: map[string]ServiceOverride{
			app.Name: svc,
		},
		Networks: map[string]Network{
			"warpgate": {External: true},
		},
	}

	yamlBytes, err := yaml.Marshal(override)
	if err != nil {
		return "", fmt.Errorf("failed to marshal override: %w", err)
	}

	return string(yamlBytes), nil
}

func buildTraefikLabels(app *config.AppConfig, networking *config.NetworkingConfig) map[string]string {
	labels := make(map[string]string)

	if len(app.Domains) == 0 {
		return labels
	}

	labels["traefik.enable"] = "true"

	for i, domain := range app.Domains {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf("-%d", i)
		}

		routerName := app.Name + suffix
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", routerName)] = fmt.Sprintf("Host(`%s`)", domain)
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", routerName)] = strings.Join(networking.Traefik.EntryPoints, ",")

		if networking.Traefik.ACME.Enabled {
			labels[fmt.Sprintf("traefik.http.routers.%s.tls", routerName)] = "true"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", routerName)] = networking.Traefik.ACME.Provider
		}
	}

	if app.Port > 0 {
		labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", app.Name)] = fmt.Sprintf("%d", app.Port)
	}

	return labels
}

func buildInternalLabels(app *config.AppConfig, labels map[string]string) {
	if app.Internal == "" || app.Port == 0 {
		return
	}

	routerName := app.Name + "-internal"
	labels["traefik.http.routers."+routerName+".rule"] = fmt.Sprintf("Host(`%s`)", app.Internal)
	labels["traefik.http.routers."+routerName+".entrypoints"] = "internal"
	labels["traefik.http.routers."+routerName+".service"] = routerName
	labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName)] = fmt.Sprintf("%d", app.Port)
}

// GenerateTraefikCompose creates the Traefik service compose file for bootstrap.
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
			fmt.Sprintf("--certificatesresolvers.%s.acme.tlschallenge=true", resolver),
			fmt.Sprintf("--certificatesresolvers.%s.acme.email=%s", resolver, networking.Traefik.ACME.Email),
			fmt.Sprintf("--certificatesresolvers.%s.acme.storage=/letsencrypt/acme.json", resolver),
		)
		if networking.Traefik.ACME.Staging {
			cmd = append(cmd,
				fmt.Sprintf("--certificatesresolvers.%s.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory", resolver),
			)
		}
	}

	cmd = append(cmd,
		"--entrypoints.internal.address=:8080",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--providers.file.watch=true",
	)
	ports = append(ports, "8080:8080")

	type traefikService struct {
		Image       string            `yaml:"image"`
		Restart     string            `yaml:"restart"`
		Command     []string          `yaml:"command"`
		Ports       []string          `yaml:"ports"`
		Volumes     []string          `yaml:"volumes"`
		Networks    []string          `yaml:"networks"`
		Environment map[string]string `yaml:"environment,omitempty"`
	}

	type traefikCompose struct {
		Services map[string]traefikService `yaml:"services"`
		Networks map[string]Network        `yaml:"networks"`
		Volumes  map[string]struct{}       `yaml:"volumes"`
	}

	compose := traefikCompose{
		Services: map[string]traefikService{
			"traefik": {
				Image:   "traefik:v3.4",
				Restart: "unless-stopped",
				Command: cmd,
				Ports:   ports,
				Volumes: []string{
					"/var/run/docker.sock:/var/run/docker.sock:ro",
					"traefik-acme:/letsencrypt",
					"/opt/warpgate/traefik/dynamic:/etc/traefik/dynamic:ro",
				},
				Networks: []string{"warpgate"},
				Environment: map[string]string{
					"DOCKER_API_VERSION": "1.45",
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

	yamlBytes, err := yaml.Marshal(compose)
	if err != nil {
		return "", fmt.Errorf("failed to marshal traefik compose: %w", err)
	}

	return string(yamlBytes), nil
}
