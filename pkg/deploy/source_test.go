package deploy

import (
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestBuildGitHubRawURL(t *testing.T) {
	tests := []struct {
		name    string
		source  *config.SourceConfig
		want    string
		wantErr bool
	}{
		{
			name: "simple github.com/owner/repo",
			source: &config.SourceConfig{
				Repo:        "github.com/pangobit/brighter",
				Ref:         "v1.0.0",
				ComposePath: "deploy/compose.yml",
			},
			want: "https://raw.githubusercontent.com/pangobit/brighter/v1.0.0/deploy/compose.yml",
		},
		{
			name: "https://github.com/owner/repo",
			source: &config.SourceConfig{
				Repo:        "https://github.com/pangobit/brighter",
				Ref:         "main",
				ComposePath: "compose.yml",
			},
			want: "https://raw.githubusercontent.com/pangobit/brighter/main/compose.yml",
		},
		{
			name: "owner/repo only",
			source: &config.SourceConfig{
				Repo:        "pangobit/brighter",
				Ref:         "abc123",
				ComposePath: "docker-compose.yml",
			},
			want: "https://raw.githubusercontent.com/pangobit/brighter/abc123/docker-compose.yml",
		},
		{
			name: "default compose path",
			source: &config.SourceConfig{
				Repo: "github.com/pangobit/brighter",
				Ref:  "v2.0.0",
			},
			want: "https://raw.githubusercontent.com/pangobit/brighter/v2.0.0/compose.yml",
		},
		{
			name:    "nil source",
			source:  nil,
			wantErr: true,
		},
		{
			name: "invalid repo format - no slash",
			source: &config.SourceConfig{
				Repo: "invalid",
				Ref:  "main",
			},
			wantErr: true,
		},
		{
			name: "http:// stripped",
			source: &config.SourceConfig{
				Repo:        "http://github.com/pangobit/brighter",
				Ref:         "v1.0",
				ComposePath: "compose.yaml",
			},
			want: "https://raw.githubusercontent.com/pangobit/brighter/v1.0/compose.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildGitHubRawURL(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildGitHubRawURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("BuildGitHubRawURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		secrets map[string]string
		want    map[string]string
	}{
		{
			name:    "empty both",
			env:     nil,
			secrets: nil,
			want:    map[string]string{},
		},
		{
			name: "env only",
			env:  map[string]string{"A": "1", "B": "2"},
			want: map[string]string{"A": "1", "B": "2"},
		},
		{
			name:    "secrets only",
			secrets: map[string]string{"SECRET": "shh"},
			want:    map[string]string{"SECRET": "shh"},
		},
		{
			name:    "env and secrets merged",
			env:     map[string]string{"A": "1"},
			secrets: map[string]string{"B": "2"},
			want:    map[string]string{"A": "1", "B": "2"},
		},
		{
			name:    "secrets override env on collision",
			env:     map[string]string{"A": "env-value", "B": "shared"},
			secrets: map[string]string{"A": "secret-value", "C": "new"},
			want:    map[string]string{"A": "secret-value", "B": "shared", "C": "new"},
		},
		{
			name:    "empty env with secrets",
			env:     map[string]string{},
			secrets: map[string]string{"KEY": "val"},
			want:    map[string]string{"KEY": "val"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeEnvironment(tt.env, tt.secrets)
			if len(got) != len(tt.want) {
				t.Errorf("mergeEnvironment() got %d keys, want %d", len(got), len(tt.want))
			}
			for k, wantVal := range tt.want {
				if got[k] != wantVal {
					t.Errorf("mergeEnvironment()[%q] = %q, want %q", k, got[k], wantVal)
				}
			}
		})
	}
}
