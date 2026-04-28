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

// GenerateReleaseServiceInternalRoute creates a Traefik dynamic file config for
// cross-node routing of a first-class release service.
func GenerateReleaseServiceInternalRoute(app *config.AppConfig, serviceName string, service config.ReleaseServiceConfig, cluster *config.ClusterConfig) (string, error) {
	ie := service.EffectiveExpose().Internal
	if ie == nil || service.Port == 0 {
		return "", nil
	}

	entrypointName := app.Name + "-" + serviceName + "-internal"
	if serviceName == app.Name {
		entrypointName = app.Name + "-internal"
	}
	entrypoint := entrypointName
	if serviceName == app.Name {
		entrypoint = "internal"
	}
	return generateInternalRouteConfig(
		entrypointName,
		ie.Hostname,
		entrypoint,
		service.Port,
		app.GetTargetNodes(cluster.Nodes),
		cluster,
	)
}

// GenerateReleaseServiceShadowInternalRoute creates a Traefik dynamic file
// config for the shadow version of a first-class release service.
func GenerateReleaseServiceShadowInternalRoute(app *config.AppConfig, serviceName string, service config.ReleaseServiceConfig, cluster *config.ClusterConfig) (string, error) {
	ie := service.EffectiveExpose().Internal
	if ie == nil || service.Port == 0 {
		return "", nil
	}

	shadowHostname := "shadow-" + ie.Hostname
	entrypointName := app.Name + "-" + serviceName + "-shadow-internal"
	entrypoint := app.Name + "-" + serviceName + "-internal"
	if serviceName == app.Name {
		entrypointName = app.Name + "-shadow-internal"
		entrypoint = "internal"
	}
	return generateInternalRouteConfig(
		entrypointName,
		shadowHostname,
		entrypoint,
		service.Port,
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
