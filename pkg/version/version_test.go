package version

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "dev", in: "dev", want: "dev"},
		{name: "devel", in: "(devel)", want: "(devel)"},
		{name: "with v prefix", in: "v1.2.3", want: "v1.2.3"},
		{name: "without v prefix", in: "1.2.3", want: "v1.2.3"},
		{name: "trim spaces", in: " v1.0.0 ", want: "v1.0.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.in); got != tc.want {
				t.Fatalf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCurrentUsesInjectedVersion(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	Version = "v9.9.9"
	if got := Current(); got != "v9.9.9" {
		t.Fatalf("Current() = %q, want v9.9.9", got)
	}
}

func TestCurrentAddsVersionPrefix(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	Version = "2.0.0"
	if got := Current(); got != "v2.0.0" {
		t.Fatalf("Current() = %q, want v2.0.0", got)
	}
}

func TestPlatform(t *testing.T) {
	if got := Platform(); got == "" {
		t.Fatal("Platform() returned empty string")
	}
}
