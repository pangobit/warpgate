// Package compose generates Docker Compose files from warpgate configuration.
package compose

import (
	"fmt"
	"strings"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

// Project represents a Docker Compose project for all apps on a specific node.
type Project struct {
	// ProjectName is the Docker Compose project name.
	ProjectName string
	// Apps is the list of app configurations targeted at this node.
	Apps []*config.AppConfig
	// Node is the target node configuration.
	Node *config.NodeConfig
	// Networking holds the cluster networking settings.
	Networking config.NetworkingConfig
}

// ComposeFile represents a Docker Compose file structure.
type ComposeFile struct {
	// Services maps service names to their definitions.
	Services map[string]Service `yaml:"services"`
	// Networks maps network names to their definitions.
	Networks map[string]Network `yaml:"networks,omitempty"`
	// Volumes maps volume names to their definitions.
	Volumes map[string]Volume `yaml:"volumes,omitempty"`
}

// DependsOnCondition specifies when a dependency is considered satisfied.
type DependsOnCondition struct {
	// Condition is the dependency condition (e.g. "service_started", "service_completed_successfully").
	Condition string `yaml:"condition"`
}

// Service represents a Docker Compose service definition.
type Service struct {
	// Image is the Docker image reference.
	Image string `yaml:"image"`
	// Command overrides the container command (string for shell form, []string for exec form).
	Command interface{} `yaml:"command,omitempty"`
	// ContainerName sets an explicit container name.
	ContainerName string `yaml:"container_name,omitempty"`
	// Restart is the restart policy.
	Restart string `yaml:"restart,omitempty"`
	// Environment holds environment variables.
	Environment map[string]string `yaml:"environment,omitempty"`
	// Ports is the list of port mappings.
	Ports []string `yaml:"ports,omitempty"`
	// Volumes is the list of volume mounts.
	Volumes []string `yaml:"volumes,omitempty"`
	// Networks is the list of networks to attach to.
	Networks []string `yaml:"networks,omitempty"`
	// Labels holds container labels (used for Traefik routing).
	Labels map[string]string `yaml:"labels,omitempty"`
	// HealthCheck defines the container health check.
	HealthCheck *HealthCheck `yaml:"healthcheck,omitempty"`
	// Deploy holds deployment settings.
	Deploy *DeployConfig `yaml:"deploy,omitempty"`
	// DependsOn maps dependency service names to their conditions.
	DependsOn map[string]DependsOnCondition `yaml:"depends_on,omitempty"`
}

// HealthCheck defines a Docker Compose health check.
type HealthCheck struct {
	// Test is the health check command.
	Test []string `yaml:"test"`
	// Interval is the time between checks.
	Interval string `yaml:"interval,omitempty"`
	// Timeout is the check timeout.
	Timeout string `yaml:"timeout,omitempty"`
	// Retries is the failure count before unhealthy.
	Retries int `yaml:"retries,omitempty"`
}

// DeployConfig holds Docker Compose deploy settings.
type DeployConfig struct {
	// Resources holds CPU and memory constraints.
	Resources Resources `yaml:"resources,omitempty"`
	// Replicas is the number of container instances.
	Replicas int `yaml:"replicas,omitempty"`
}

// Resources holds CPU and memory constraints.
type Resources struct {
	// Limits holds the upper bound for CPU and memory.
	Limits ResourceLimits `yaml:"limits,omitempty"`
	// Reservations holds the guaranteed CPU and memory.
	Reservations ResourceLimits `yaml:"reservations,omitempty"`
}

// ResourceLimits holds a single set of CPU/memory values.
type ResourceLimits struct {
	// CPUs is the CPU allocation (e.g. "0.5").
	CPUs string `yaml:"cpus,omitempty"`
	// Memory is the memory allocation (e.g. "512M").
	Memory string `yaml:"memory,omitempty"`
}

// Network represents a Docker Compose network definition.
type Network struct {
	// External indicates the network is managed outside this compose file.
	External bool `yaml:"external,omitempty"`
	// Driver is the network driver.
	Driver string `yaml:"driver,omitempty"`
}

// Volume represents a Docker Compose volume definition.
type Volume struct {
	// External indicates the volume is managed outside this compose file.
	External bool `yaml:"external,omitempty"`
	// Driver is the volume driver.
	Driver string `yaml:"driver,omitempty"`
}

// NewProject creates a compose project generator for a node and all its apps.
func NewProject(projectName string, apps []*config.AppConfig, node *config.NodeConfig, networking config.NetworkingConfig) *Project {
	return &Project{
		ProjectName: projectName,
		Apps:        apps,
		Node:        node,
		Networking:  networking,
	}
}

// Generate creates the Docker Compose YAML for all apps on this node.
func (p *Project) Generate() (string, error) {
	compose := &ComposeFile{
		Services: make(map[string]Service),
		Networks: make(map[string]Network),
		Volumes:  make(map[string]Volume),
	}

	for _, app := range p.Apps {
		mainService := p.buildMainService(app)

		for _, init := range app.Init {
			initName := fmt.Sprintf("%s-%s", app.Name, init.Name)
			if mainService.DependsOn == nil {
				mainService.DependsOn = make(map[string]DependsOnCondition)
			}
			mainService.DependsOn[initName] = DependsOnCondition{Condition: "service_completed_successfully"}
		}
		compose.Services[app.Name] = mainService

		for _, sidecar := range app.Sidecars {
			sidecarName := fmt.Sprintf("%s-%s", app.Name, sidecar.Name)
			compose.Services[sidecarName] = p.buildSidecarService(app, sidecar)
		}

		for _, init := range app.Init {
			initName := fmt.Sprintf("%s-%s", app.Name, init.Name)
			compose.Services[initName] = p.buildInitService(app, init)
		}

		for _, vol := range app.Volumes {
			compose.Volumes[vol.Name] = Volume{}
		}

		p.collectVolumes(compose.Volumes, app.Sidecars, app.Init)
	}

	if len(p.Networking.Traefik.EntryPoints) > 0 {
		compose.Services["traefik"] = p.buildTraefikService()
		compose.Volumes["traefik-acme"] = Volume{}
	}

	compose.Networks["warpgate"] = Network{}

	yamlBytes, err := yaml.Marshal(compose)
	if err != nil {
		return "", fmt.Errorf("failed to marshal compose: %w", err)
	}

	return string(yamlBytes), nil
}

func (p *Project) buildMainService(app *config.AppConfig) Service {
	version := app.Version
	if version == "" {
		version = "latest"
	}

	svc := Service{
		Image:       fmt.Sprintf("%s:%s", app.Image, version),
		Restart:     "unless-stopped",
		Networks:    []string{"warpgate"},
		Environment: make(map[string]string),
		Labels:      p.buildTraefikLabels(app),
	}

	for k, v := range app.Env {
		svc.Environment[k] = v
	}

	for _, secretName := range app.Secrets {
		svc.Environment[secretName] = fmt.Sprintf("${%s}", secretName)
	}

	for _, port := range app.Ports {
		var portStr string
		if port.Host == 0 {
			portStr = fmt.Sprintf("%d", port.Container)
		} else {
			portStr = fmt.Sprintf("%d:%d", port.Host, port.Container)
		}
		if port.Protocol != "" && port.Protocol != "tcp" {
			portStr = fmt.Sprintf("%s/%s", portStr, port.Protocol)
		}
		svc.Ports = append(svc.Ports, portStr)
	}

	for _, vol := range app.Volumes {
		svc.Volumes = append(svc.Volumes, fmt.Sprintf("%s:%s", vol.Name, vol.Path))
	}

	if app.HealthCheck.Path != "" || app.HealthCheck.Command != "" {
		hc := &HealthCheck{}
		if app.HealthCheck.Command != "" {
			hc.Test = []string{"CMD-SHELL", app.HealthCheck.Command}
		} else {
			hc.Test = []string{"CMD-SHELL", fmt.Sprintf("wget --quiet --tries=1 --spider http://localhost:%d%s || exit 1", app.HealthCheck.Port, app.HealthCheck.Path)}
		}
		if app.HealthCheck.Interval != "" {
			hc.Interval = app.HealthCheck.Interval
		}
		if app.HealthCheck.Timeout != "" {
			hc.Timeout = app.HealthCheck.Timeout
		}
		if app.HealthCheck.Retries > 0 {
			hc.Retries = app.HealthCheck.Retries
		}
		svc.HealthCheck = hc
	}

	if app.Resources.CPUs != "" || app.Resources.Memory != "" {
		svc.Deploy = &DeployConfig{
			Resources: Resources{
				Reservations: ResourceLimits{
					CPUs:   app.Resources.CPUs,
					Memory: app.Resources.Memory,
				},
			},
		}
		if app.Resources.CPULimit != "" || app.Resources.MemLimit != "" {
			svc.Deploy.Resources.Limits = ResourceLimits{
				CPUs:   app.Resources.CPULimit,
				Memory: app.Resources.MemLimit,
			}
		}
	}

	if app.Replicas > 0 {
		if svc.Deploy == nil {
			svc.Deploy = &DeployConfig{}
		}
		svc.Deploy.Replicas = app.Replicas
	}

	return svc
}

func (p *Project) buildSidecarService(app *config.AppConfig, sidecar config.SidecarConfig) Service {
	svc := Service{
		Image:    sidecar.Image,
		Restart:  "unless-stopped",
		Networks: []string{"warpgate"},
		Volumes:  sidecar.Volumes,
		DependsOn: map[string]DependsOnCondition{
			app.Name: {Condition: "service_started"},
		},
	}
	if len(sidecar.Env) > 0 {
		svc.Environment = sidecar.Env
	}
	if sidecar.Command != "" {
		svc.Command = sidecar.Command
	}
	return svc
}

func (p *Project) buildInitService(app *config.AppConfig, init config.InitContainerConfig) Service {
	svc := Service{
		Image:    init.Image,
		Restart:  "no",
		Networks: []string{"warpgate"},
		Volumes:  init.Volumes,
	}
	if len(init.Env) > 0 {
		svc.Environment = init.Env
	}
	if init.Command != "" {
		svc.Command = init.Command
	}
	return svc
}

func (p *Project) buildTraefikLabels(app *config.AppConfig) map[string]string {
	labels := make(map[string]string)

	if len(app.Domains) == 0 {
		return labels
	}

	labels["traefik.enable"] = "true"

	routerName := fmt.Sprintf("%s-%s", app.Name, p.Node.ID)

	for i, domain := range app.Domains {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf("-%d", i)
		}

		labels[fmt.Sprintf("traefik.http.routers.%s%s.rule", routerName, suffix)] = fmt.Sprintf("Host(`%s`)", domain)
		labels[fmt.Sprintf("traefik.http.routers.%s%s.entrypoints", routerName, suffix)] = strings.Join(p.Networking.Traefik.EntryPoints, ",")

		if p.Networking.Traefik.ACME.Enabled {
			labels[fmt.Sprintf("traefik.http.routers.%s%s.tls", routerName, suffix)] = "true"
			labels[fmt.Sprintf("traefik.http.routers.%s%s.tls.certresolver", routerName, suffix)] = p.Networking.Traefik.ACME.Provider
		}
	}

	if len(app.Ports) > 0 {
		port := app.Ports[0].Container
		labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName)] = fmt.Sprintf("%d", port)
	}

	return labels
}

func (p *Project) buildTraefikService() Service {
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
	for _, ep := range p.Networking.Traefik.EntryPoints {
		port, ok := portMap[ep]
		if !ok {
			continue
		}
		cmd = append(cmd, fmt.Sprintf("--entrypoints.%s.address=:%d", ep, port))
		ports = append(ports, fmt.Sprintf("%d:%d", port, port))
	}

	if p.Networking.Traefik.ACME.Enabled {
		resolver := p.Networking.Traefik.ACME.Provider
		cmd = append(cmd,
			fmt.Sprintf("--certificatesresolvers.%s.acme.tlschallenge=true", resolver),
			fmt.Sprintf("--certificatesresolvers.%s.acme.email=%s", resolver, p.Networking.Traefik.ACME.Email),
			fmt.Sprintf("--certificatesresolvers.%s.acme.storage=/letsencrypt/acme.json", resolver),
		)
		if p.Networking.Traefik.ACME.Staging {
			cmd = append(cmd,
				fmt.Sprintf("--certificatesresolvers.%s.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory", resolver),
			)
		}
	}

	return Service{
		Image:   "traefik:v3.4",
		Restart: "unless-stopped",
		Command: cmd,
		Ports:   ports,
		Volumes: []string{
			"/var/run/docker.sock:/var/run/docker.sock:ro",
			"traefik-acme:/letsencrypt",
		},
		Networks: []string{"warpgate"},
	}
}

func (p *Project) collectVolumes(volumes map[string]Volume, sidecars []config.SidecarConfig, inits []config.InitContainerConfig) {
	for _, sc := range sidecars {
		for _, v := range sc.Volumes {
			volName := strings.SplitN(v, ":", 2)[0]
			if _, exists := volumes[volName]; !exists {
				volumes[volName] = Volume{}
			}
		}
	}
	for _, init := range inits {
		for _, v := range init.Volumes {
			volName := strings.SplitN(v, ":", 2)[0]
			if _, exists := volumes[volName]; !exists {
				volumes[volName] = Volume{}
			}
		}
	}
}
