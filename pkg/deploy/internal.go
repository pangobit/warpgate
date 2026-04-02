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
// routing of an internal service. It lists all target nodes' Tailscale IPs as
// backends for the service's internal hostname.
func GenerateInternalRoute(app *config.AppConfig, cluster *config.ClusterConfig) (string, error) {
	if app.Internal == "" || app.Port == 0 {
		return "", nil
	}

	targetNodes := app.GetTargetNodes(cluster.Nodes)
	var servers []internalServer
	for _, nodeID := range targetNodes {
		node := cluster.GetNode(nodeID)
		if node == nil || node.TailscaleIP == "" {
			continue
		}
		servers = append(servers, internalServer{
			URL: fmt.Sprintf("http://%s:%d", node.TailscaleIP, app.Port),
		})
	}

	if len(servers) == 0 {
		return "", nil
	}

	routerName := app.Name + "-internal"
	cfg := InternalRouteConfig{}
	cfg.HTTP.Routers = map[string]internalRouter{
		routerName: {
			Rule:        fmt.Sprintf("Host(`%s`)", app.Internal),
			Service:     routerName,
			EntryPoints: []string{"internal"},
		},
	}
	cfg.HTTP.Services = map[string]internalService{
		routerName: {
			LoadBalancer: internalLB{
				Servers: servers,
				HealthCheck: &internalHealthCheck{
					Port:     app.Port,
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
