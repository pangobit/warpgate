package deploy

import (
	"testing"
)

func TestBuildLogsCommand(t *testing.T) {
	tests := []struct {
		name string
		opts LogsOptions
		want string
	}{
		{
			name: "default tail no filters",
			opts: LogsOptions{Tail: 100},
			want: `docker ps --format '{{.Names}}' | xargs -I {} sh -c 'docker logs --tail 100 {} 2>&1 | sed "s/^/[{}] /"'`,
		},
		{
			name: "zero tail defaults to 100",
			opts: LogsOptions{},
			want: `docker ps --format '{{.Names}}' | xargs -I {} sh -c 'docker logs --tail 100 {} 2>&1 | sed "s/^/[{}] /"'`,
		},
		{
			name: "custom tail",
			opts: LogsOptions{Tail: 50},
			want: `docker ps --format '{{.Names}}' | xargs -I {} sh -c 'docker logs --tail 50 {} 2>&1 | sed "s/^/[{}] /"'`,
		},
		{
			name: "with app filter",
			opts: LogsOptions{Tail: 100, App: "brighter-platform"},
			want: `docker ps --format '{{.Names}}' --filter 'name=brighter-platform' | xargs -I {} sh -c 'docker logs --tail 100 {} 2>&1 | sed "s/^/[{}] /"'`,
		},
		{
			name: "with grep",
			opts: LogsOptions{Tail: 100, Grep: "error"},
			want: `docker ps --format '{{.Names}}' | xargs -I {} sh -c 'docker logs --tail 100 {} 2>&1 | sed "s/^/[{}] /"' | grep 'error'`,
		},
		{
			name: "app filter and grep combined",
			opts: LogsOptions{Tail: 20, App: "myapp", Grep: "panic"},
			want: `docker ps --format '{{.Names}}' --filter 'name=myapp' | xargs -I {} sh -c 'docker logs --tail 20 {} 2>&1 | sed "s/^/[{}] /"' | grep 'panic'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildLogsCommand(tt.opts)
			if got != tt.want {
				t.Errorf("BuildLogsCommand() =\n  %s\nwant:\n  %s", got, tt.want)
			}
		})
	}
}
