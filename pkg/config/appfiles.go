package config

import "strings"

// IsDeployExtraFile reports whether name is a flat app config file that Warpgate
// syncs from the infra repo and uploads during deploy.
func IsDeployExtraFile(name string) bool {
	if name == "" || strings.Contains(name, "/") {
		return false
	}
	switch name {
	case "app.yml", "compose.yml", "docker-compose.override.yml", "state.json":
		return false
	}
	if strings.HasPrefix(name, ".env") {
		return false
	}
	return true
}
