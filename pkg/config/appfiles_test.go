package config

import "testing"

func TestIsDeployExtraFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "vector.yaml", want: true},
		{name: "grafana-datasources.yaml", want: true},
		{name: "dashboard-overview.json", want: true},
		{name: "nginx.conf", want: true},
		{name: "app.yml", want: false},
		{name: "compose.yml", want: false},
		{name: "docker-compose.override.yml", want: false},
		{name: "state.json", want: false},
		{name: ".env", want: false},
		{name: ".env.observability", want: false},
		{name: ".gitignore", want: false},
		{name: ".DS_Store", want: false},
		{name: "config/vector.yaml", want: false},
		{name: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsDeployExtraFile(test.name); got != test.want {
				t.Fatalf("IsDeployExtraFile(%q) = %v, want %v", test.name, got, test.want)
			}
		})
	}
}
