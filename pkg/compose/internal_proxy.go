package compose

import (
	"fmt"
	"sort"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

var reservedInternalProxyPorts = map[int]bool{
	80:   true,
	443:  true,
	8080: true,
}

// InternalProxyConfig holds parameters for generating the internal Traefik compose.
type InternalProxyConfig struct {
	// PrivateIP is the node's private network IP to bind entrypoints to.
	PrivateIP string
	// ProxyNetwork is the optional Docker network shared with trusted public services.
	ProxyNetwork string
	// Entrypoints maps entrypoint names to port numbers.
	Entrypoints map[string]int
}

// CollectInternalEntrypoints scans all apps targeting a node and returns the
// entrypoints needed for the internal proxy. Always includes the base "internal"
// entrypoint on port 8080 for cross-node routing. Only creates per-service
// entrypoints when expose.private is explicitly configured.
func CollectInternalEntrypoints(apps []*config.AppConfig) map[string]int {
	eps := map[string]int{"internal": 8080}
	for _, app := range apps {
		for name, service := range app.Release.Services {
			if pe := service.EffectiveExpose().Private; pe != nil {
				if reservedInternalProxyPorts[pe.Port] {
					continue
				}
				epName := app.Name + "-" + name + "-internal"
				if name == app.Name {
					epName = app.Name + "-port-internal"
				}
				eps[epName] = pe.Port
			}
		}
	}
	return eps
}

// GenerateInternalProxyCompose creates the internal Traefik compose YAML.
// The internal proxy binds only to the node's private IP, making it
// accessible from the private network but not from the public internet.
func GenerateInternalProxyCompose(cfg *InternalProxyConfig) (string, error) {
	cmd := []string{
		"--providers.docker=true",
		"--providers.docker.network=warpgate",
		"--providers.docker.exposedbydefault=false",
		"--providers.docker.defaultRule=",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--providers.file.watch=true",
	}

	var epNames []string
	for name := range cfg.Entrypoints {
		epNames = append(epNames, name)
	}
	sort.Strings(epNames)

	var ports []string
	for _, name := range epNames {
		port := cfg.Entrypoints[name]
		cmd = append(cmd, fmt.Sprintf("--entrypoints.%s.address=:%d", name, port))
		ports = append(ports, fmt.Sprintf("%s:%d:%d", cfg.PrivateIP, port, port))
	}

	type proxyService struct {
		Image       string   `yaml:"image"`
		Restart     string   `yaml:"restart"`
		Command     []string `yaml:"command"`
		Ports       []string `yaml:"ports"`
		Volumes     []string `yaml:"volumes"`
		Networks    []string `yaml:"networks"`
		Environment []string `yaml:"environment,omitempty"`
	}

	type proxyCompose struct {
		Services map[string]proxyService `yaml:"services"`
		Networks map[string]Network      `yaml:"networks"`
	}

	networks := map[string]Network{
		"warpgate": {External: true},
	}
	proxyNetworks := []string{"warpgate"}
	if cfg.ProxyNetwork != "" {
		networks[cfg.ProxyNetwork] = Network{External: true}
		proxyNetworks = append(proxyNetworks, cfg.ProxyNetwork)
	}

	compose := proxyCompose{
		Services: map[string]proxyService{
			"traefik": {
				Image:   "traefik:v3.6",
				Restart: "unless-stopped",
				Command: cmd,
				Ports:   ports,
				Volumes: []string{
					"/var/run/docker.sock:/var/run/docker.sock:ro",
					"/opt/warpgate/traefik/dynamic:/etc/traefik/dynamic:ro",
				},
				Networks: proxyNetworks,
				Environment: []string{
					"DOCKER_API_VERSION=1.45",
				},
			},
		},
		Networks: networks,
	}

	yamlBytes, err := yaml.Marshal(compose)
	if err != nil {
		return "", fmt.Errorf("failed to marshal internal proxy compose: %w", err)
	}

	return string(yamlBytes), nil
}
