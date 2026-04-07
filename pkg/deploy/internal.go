package deploy

import (
	"fmt"

	"github.com/pangobit/warpgate/pkg/config"
	"gopkg.in/yaml.v3"
)

// InternalRouteConfig represents a Traefik file-provider dynamic config for
// routing internal service-to-service traffic across nodes.
type InternalRouteConfig struct {
	HTTP struct {
		Routers  map[string]internalRouter  `yaml:"routers"`
		Services map[string]internalService `yaml:"services"`
	} `yaml:"http"`
}

type internalRouter struct {
	Rule        string   `yaml:"rule"`
	Service     string   `yaml:"service"`
	EntryPoints []string `yaml:"entryPoints"`
}

type internalService struct {
	LoadBalancer internalLB `yaml:"loadBalancer"`
}

type internalLB struct {
	HealthCheck *internalHealthCheck `yaml:"healthCheck,omitempty"`
	Servers     []internalServer     `yaml:"servers"`
}

type internalHealthCheck struct {
	Path     string `yaml:"path,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Interval string `yaml:"interval,omitempty"`
}

type internalServer struct {
	URL string `yaml:"url"`
}

// GenerateInternalRoute creates a Traefik dynamic file config for cross-node
// routing of an internal service. It lists all target nodes' private IPs as
// backends for the service's internal hostname.
func GenerateInternalRoute(app *config.AppConfig, cluster *config.ClusterConfig) (string, error) {
	ie := app.EffectiveExpose().Internal
	if ie == nil || app.Port == 0 {
		return "", nil
	}

	return generateInternalRouteConfig(
		app.Name+"-internal",
		ie.Hostname,
		"internal",
		app.Port,
		app.GetTargetNodes(cluster.Nodes),
		cluster,
	)
}

// GenerateSidecarInternalRoute creates a Traefik dynamic file config for cross-node
// routing of a sidecar's internal hostname.
func GenerateSidecarInternalRoute(app *config.AppConfig, sidecarName string, sidecar config.SidecarConfig, cluster *config.ClusterConfig) (string, error) {
	ie := sidecar.EffectiveExpose().Internal
	if ie == nil || sidecar.Port == 0 {
		return "", nil
	}

	entrypointName := app.Name + "-" + sidecarName + "-internal"
	return generateInternalRouteConfig(
		entrypointName,
		ie.Hostname,
		entrypointName,
		sidecar.Port,
		app.GetTargetNodes(cluster.Nodes),
		cluster,
	)
}

// GenerateShadowInternalRoute creates a Traefik dynamic file config for the
// shadow version of an internal service. The shadow hostname uses the
// "shadow-{app}.internal" convention.
func GenerateShadowInternalRoute(app *config.AppConfig, cluster *config.ClusterConfig) (string, error) {
	ie := app.EffectiveExpose().Internal
	if ie == nil || app.Port == 0 {
		return "", nil
	}

	shadowHostname := "shadow-" + ie.Hostname
	return generateInternalRouteConfig(
		app.Name+"-shadow-internal",
		shadowHostname,
		"internal",
		app.Port,
		app.GetTargetNodes(cluster.Nodes),
		cluster,
	)
}

func generateInternalRouteConfig(routerName, hostname, entrypoint string, port int, targetNodes []string, cluster *config.ClusterConfig) (string, error) {
	var servers []internalServer
	for _, nodeID := range targetNodes {
		node := cluster.GetNode(nodeID)
		if node == nil || node.PrivateIP == "" {
			continue
		}
		servers = append(servers, internalServer{
			URL: fmt.Sprintf("http://%s:%d", node.PrivateIP, port),
		})
	}

	if len(servers) == 0 {
		return "", nil
	}

	cfg := InternalRouteConfig{}
	cfg.HTTP.Routers = map[string]internalRouter{
		routerName: {
			Rule:        "Host(`" + hostname + "`)",
			Service:     routerName,
			EntryPoints: []string{entrypoint},
		},
	}
	cfg.HTTP.Services = map[string]internalService{
		routerName: {
			LoadBalancer: internalLB{
				Servers: servers,
				HealthCheck: &internalHealthCheck{
					Port:     port,
					Interval: "5s",
				},
			},
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal internal route config: %w", err)
	}

	return string(data), nil
}
