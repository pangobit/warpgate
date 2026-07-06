package github

import "testing"

func TestIsAppExtraFilePath(t *testing.T) {
	tests := []struct {
		root    string
		appName string
		path    string
		want    bool
	}{
		{root: "prod/", appName: "observability", path: "prod/apps/observability/vector.yaml", want: true},
		{root: "prod/", appName: "observability", path: "prod/apps/observability/app.yml", want: false},
		{root: "prod/", appName: "observability", path: "prod/apps/observability/compose.yml", want: false},
		{root: "prod/", appName: "observability", path: "prod/apps/observability/releases/latest.json", want: false},
		{root: "", appName: "api", path: "apps/api/notes.txt", want: true},
		{root: "", appName: "api", path: "apps/other/notes.txt", want: false},
		{root: "prod/", appName: "observability", path: "prod/apps/observability/config/vector.yaml", want: false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := isAppExtraFilePath(test.root, test.appName, test.path); got != test.want {
				t.Fatalf("isAppExtraFilePath(%q, %q, %q) = %v, want %v", test.root, test.appName, test.path, got, test.want)
			}
		})
	}
}
