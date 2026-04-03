package compose

import (
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestCollectInternalEntrypoints(t *testing.T) {
	tests := []struct {
		name string
		apps []*config.AppConfig
		want map[string]int
	}{
		{
			name: "no apps still has base entrypoint",
			apps: nil,
			want: map[string]int{"internal": 8080},
		},
		{
			name: "app with expose.private",
			apps: []*config.AppConfig{
				{
					Name: "auth",
					Port: 8085,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 8085},
					},
				},
			},
			want: map[string]int{
				"internal":           8080,
				"auth-port-internal": 8085,
			},
		},
		{
			name: "app without expose.private is skipped",
			apps: []*config.AppConfig{
				{Name: "auth", Port: 8085},
			},
			want: map[string]int{
				"internal": 8080,
			},
		},
		{
			name: "app with sidecar expose.private",
			apps: []*config.AppConfig{
				{
					Name: "brighter-platform",
					Port: 8083,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 8083},
					},
					Sidecars: map[string]config.SidecarConfig{
						"admin": {
							Port: 8087,
							Expose: &config.ExposeConfig{
								Private: &config.PrivateExpose{Port: 8087},
							},
						},
					},
				},
			},
			want: map[string]int{
				"brighter-platform-port-internal":  8083,
				"internal":                         8080,
				"brighter-platform-admin-internal": 8087,
			},
		},
		{
			name: "multiple apps with sidecars",
			apps: []*config.AppConfig{
				{
					Name: "platform",
					Port: 8083,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 8083},
					},
					Sidecars: map[string]config.SidecarConfig{
						"admin": {
							Port: 8087,
							Expose: &config.ExposeConfig{
								Private: &config.PrivateExpose{Port: 8087},
							},
						},
					},
				},
				{
					Name: "auth",
					Port: 8085,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 8085},
					},
					Sidecars: map[string]config.SidecarConfig{
						"metrics": {
							Port: 9090,
							Expose: &config.ExposeConfig{
								Private: &config.PrivateExpose{Port: 9090},
							},
						},
					},
				},
			},
			want: map[string]int{
				"internal":                8080,
				"platform-port-internal":  8083,
				"platform-admin-internal": 8087,
				"auth-port-internal":      8085,
				"auth-metrics-internal":   9090,
			},
		},
		{
			name: "sidecar without expose is skipped",
			apps: []*config.AppConfig{
				{
					Name: "app",
					Port: 8081,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 8081},
					},
					Sidecars: map[string]config.SidecarConfig{
						"worker": {
							Port: 9000,
							Expose: &config.ExposeConfig{
								Internal: &config.InternalExpose{Hostname: "worker.internal"},
							},
						},
					},
				},
			},
			want: map[string]int{
				"internal":          8080,
				"app-port-internal": 8081,
			},
		},
		{
			name: "reserved private ports are skipped",
			apps: []*config.AppConfig{
				{
					Name: "pangobit",
					Port: 80,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 80},
					},
				},
				{
					Name: "gateway",
					Port: 443,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 443},
					},
				},
				{
					Name: "api",
					Port: 8080,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 8080},
					},
				},
				{
					Name: "auth",
					Port: 8085,
					Expose: &config.ExposeConfig{
						Private: &config.PrivateExpose{Port: 8085},
					},
				},
			},
			want: map[string]int{
				"internal":           8080,
				"auth-port-internal": 8085,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectInternalEntrypoints(tt.apps)
			if len(got) != len(tt.want) {
				t.Fatalf("CollectInternalEntrypoints() = %v, want %v", got, tt.want)
			}
			for name, port := range tt.want {
				if got[name] != port {
					t.Errorf("entrypoint %s: got port %d, want %d", name, got[name], port)
				}
			}
		})
	}
}

func TestGenerateInternalProxyCompose(t *testing.T) {
	cfg := &InternalProxyConfig{
		PrivateIP: "100.95.115.81",
		Entrypoints: map[string]int{
			"internal": 8080,
		},
	}

	output, err := GenerateInternalProxyCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateInternalProxyCompose() error: %v", err)
	}

	if !strings.Contains(output, "traefik:v3.6") {
		t.Error("expected traefik:v3.6 image")
	}
	if !strings.Contains(output, "100.95.115.81:8080:8080") {
		t.Error("expected internal port bound to private IP")
	}
	if !strings.Contains(output, "providers.docker=true") {
		t.Error("expected Docker provider")
	}
	if !strings.Contains(output, "providers.file.directory") {
		t.Error("expected file provider for dynamic configs")
	}
	if !strings.Contains(output, "/opt/warpgate/traefik/dynamic") {
		t.Error("expected dynamic config volume mount")
	}
	if !strings.Contains(output, "DOCKER_API_VERSION") {
		t.Error("expected Docker API version env")
	}
}

func TestGenerateInternalProxyComposeMultipleEntrypoints(t *testing.T) {
	cfg := &InternalProxyConfig{
		PrivateIP: "100.95.115.81",
		Entrypoints: map[string]int{
			"brighter-platform-port-internal":  8083,
			"internal":                         8080,
			"brighter-platform-admin-internal": 8087,
		},
	}

	output, err := GenerateInternalProxyCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateInternalProxyCompose() error: %v", err)
	}

	if !strings.Contains(output, "100.95.115.81:8080:8080") {
		t.Error("expected internal port 8080 bound to private IP")
	}
	if !strings.Contains(output, "100.95.115.81:8083:8083") {
		t.Error("expected app port 8083 bound to private IP")
	}
	if !strings.Contains(output, "100.95.115.81:8087:8087") {
		t.Error("expected admin port 8087 bound to private IP")
	}
	if !strings.Contains(output, "entrypoints.brighter-platform-port-internal.address=:8083") {
		t.Error("expected app entrypoint definition")
	}
	if !strings.Contains(output, "entrypoints.brighter-platform-admin-internal.address=:8087") {
		t.Error("expected admin entrypoint definition")
	}
}
