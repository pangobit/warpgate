package compose

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// composeFile represents a minimal parse of a Docker Compose file.
type composeFile struct {
	// Services maps service names to their definitions.
	Services map[string]interface{} `yaml:"services"`
}

// ParseComposeServices extracts service names from a Docker Compose file.
func ParseComposeServices(content string) ([]string, error) {
	var cf composeFile
	if err := yaml.Unmarshal([]byte(content), &cf); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	var names []string
	for name := range cf.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
