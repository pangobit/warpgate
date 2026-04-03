// Package compose generates Docker Compose override files for Traefik integration.
package compose

import (
	"fmt"
	"strconv"
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
	// Networks attaches the service to networks with optional aliases.
	Networks map[string]ServiceNetworkConfig `yaml:"networks,omitempty"`
	// ExtraHosts maps internal hostnames to host-gateway for service discovery.
	ExtraHosts []string `yaml:"extra_hosts,omitempty"`
}

// ServiceNetworkConfig holds per-service network settings.
type ServiceNetworkConfig struct {
	// Aliases are additional hostnames for the service on this network.
	Aliases []string `yaml:"aliases,omitempty"`
}

// Network represents a Docker Compose network definition.
type Network struct {
	// External indicates the network is managed outside this compose file.
	External bool `yaml:"external,omitempty"`
}

// GenerateOverride creates a docker-compose.override.yml that injects the image tag,
// Traefik labels, the warpgate network with aliases, and strips host port bindings.
// composeContent is the raw compose.yml content, used to discover all service names.
func GenerateOverride(app *config.AppConfig, networking *config.NetworkingConfig, internalHosts []string, composeContent string) (string, error) {
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
		extraHosts = append(extraHosts, host+":host-gateway")
	}

	services := make(map[string]ServiceOverride)

	for _, svcName := range serviceNames {
		svc := ServiceOverride{
			Networks: map[string]ServiceNetworkConfig{
				"warpgate": {Aliases: []string{svcName}},
			},
			ExtraHosts: extraHosts,
		}

		if svcName == app.Name {
			svc.Image = app.Image + ":" + version

			labels := buildTraefikLabels(app, networking)
			buildPrivatePortLabels(app, labels)
			buildInternalLabels(app, labels)
			if len(labels) > 0 {
				svc.Labels = labels
			}
		}

		if sidecar, ok := app.Sidecars[svcName]; ok {
			labels := buildSidecarLabels(app.Name, svcName, sidecar)
			if len(labels) > 0 {
				svc.Labels = labels
			}
		}

		services[svcName] = svc
	}

	override := &OverrideFile{
		Services: services,
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

	pub := app.EffectiveExpose().Public
	if pub == nil || len(pub.Domains) == 0 {
		return labels
	}

	labels["traefik.enable"] = "true"

	for i, domain := range pub.Domains {
		suffix := ""
		if i > 0 {
			suffix = "-" + strconv.Itoa(i)
		}

		routerName := app.Name + suffix
		labels["traefik.http.routers."+routerName+".rule"] = "Host(`" + domain + "`)"
		labels["traefik.http.routers."+routerName+".entrypoints"] = strings.Join(networking.Traefik.EntryPoints, ",")

		if networking.Traefik.ACME.Enabled {
			labels["traefik.http.routers."+routerName+".tls"] = "true"
			labels["traefik.http.routers."+routerName+".tls.certresolver"] = networking.Traefik.ACME.Provider
		}
	}

	if app.Port > 0 {
		labels["traefik.http.services."+app.Name+".loadbalancer.server.port"] = strconv.Itoa(app.Port)
	}

	return labels
}

func buildInternalLabels(app *config.AppConfig, labels map[string]string) {
	ie := app.EffectiveExpose().Internal
	if ie == nil || app.Port == 0 {
		return
	}

	routerName := app.Name + "-internal"
	labels["traefik.enable"] = "true"
	labels["traefik.http.routers."+routerName+".rule"] = "Host(`" + ie.Hostname + "`)"
	labels["traefik.http.routers."+routerName+".entrypoints"] = "internal"
	labels["traefik.http.routers."+routerName+".service"] = routerName
	labels["traefik.http.services."+routerName+".loadbalancer.server.port"] = strconv.Itoa(app.Port)
}

func buildPrivatePortLabels(app *config.AppConfig, labels map[string]string) {
	pe := app.EffectiveExpose().Private
	if pe == nil {
		return
	}

	routerName := app.Name + "-port-internal"
	labels["traefik.enable"] = "true"
	labels["traefik.http.routers."+routerName+".rule"] = "PathPrefix(`/`)"
	labels["traefik.http.routers."+routerName+".entrypoints"] = routerName
	labels["traefik.http.routers."+routerName+".service"] = routerName
	labels["traefik.http.services."+routerName+".loadbalancer.server.port"] = strconv.Itoa(app.Port)
}

func buildSidecarLabels(appName, sidecarName string, sidecar config.SidecarConfig) map[string]string {
	labels := make(map[string]string)

	pe := sidecar.EffectiveExpose().Private
	if pe == nil {
		return labels
	}

	routerName := appName + "-" + sidecarName + "-internal"
	labels["traefik.enable"] = "true"
	labels["traefik.http.routers."+routerName+".rule"] = "PathPrefix(`/`)"
	labels["traefik.http.routers."+routerName+".entrypoints"] = routerName
	labels["traefik.http.routers."+routerName+".service"] = routerName
	labels["traefik.http.services."+routerName+".loadbalancer.server.port"] = strconv.Itoa(sidecar.Port)
	return labels
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

	type traefikService struct {
		Image       string   `yaml:"image"`
		Restart     string   `yaml:"restart"`
		Command     []string `yaml:"command"`
		Ports       []string `yaml:"ports"`
		Volumes     []string `yaml:"volumes"`
		Networks    []string `yaml:"networks"`
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

	yamlBytes, err := yaml.Marshal(compose)
	if err != nil {
		return "", fmt.Errorf("failed to marshal traefik compose: %w", err)
	}

	return string(yamlBytes), nil
}
