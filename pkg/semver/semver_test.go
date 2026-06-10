package semver

import "testing"

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr bool
	}{
		{raw: "*"},
		{raw: ""},
		{raw: "1.2.3"},
		{raw: "v1.2.3"},
		{raw: "~1.2"},
		{raw: "~1.2.3"},
		{raw: "^1"},
		{raw: "^1.2.3"},
		{raw: "1.2.3-rc.1"},
		{raw: "1.2", wantErr: true},
		{raw: "1", wantErr: true},
		{raw: "latest", wantErr: true},
		{raw: "~latest", wantErr: true},
		{raw: "^", wantErr: true},
		{raw: "~", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			_, err := ParseConstraint(test.raw)
			if test.wantErr && err == nil {
				t.Fatalf("ParseConstraint(%q) expected error, got nil", test.raw)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("ParseConstraint(%q) unexpected error: %v", test.raw, err)
			}
		})
	}
}

func TestConstraintMatches(t *testing.T) {
	tests := []struct {
		constraint string
		tag        string
		want       bool
	}{
		{constraint: "*", tag: "1.2.3", want: true},
		{constraint: "*", tag: "v1.2.3", want: true},
		{constraint: "*", tag: "latest", want: false},
		{constraint: "*", tag: "1.2", want: false},
		{constraint: "*", tag: "1", want: false},
		{constraint: "*", tag: "1.2.3-rc.1", want: false},
		{constraint: "1.2.3", tag: "1.2.3", want: true},
		{constraint: "1.2.3", tag: "v1.2.3", want: true},
		{constraint: "1.2.3", tag: "1.2.4", want: false},
		{constraint: "1.2.3-rc.1", tag: "1.2.3-rc.1", want: true},
		{constraint: "~1.2", tag: "1.2.0", want: true},
		{constraint: "~1.2", tag: "1.2.9", want: true},
		{constraint: "~1.2", tag: "1.3.0", want: false},
		{constraint: "~1.2", tag: "2.2.0", want: false},
		{constraint: "~1.2.5", tag: "1.2.4", want: false},
		{constraint: "~1.2.5", tag: "1.2.6", want: true},
		{constraint: "~1.2", tag: "1.2.3-rc.1", want: false},
		{constraint: "^1", tag: "1.0.0", want: true},
		{constraint: "^1", tag: "1.9.9", want: true},
		{constraint: "^1", tag: "2.0.0", want: false},
		{constraint: "^1.2.3", tag: "1.2.2", want: false},
		{constraint: "^1.2.3", tag: "1.3.0", want: true},
	}
	for _, test := range tests {
		t.Run(test.constraint+"/"+test.tag, func(t *testing.T) {
			constraint, err := ParseConstraint(test.constraint)
			if err != nil {
				t.Fatalf("ParseConstraint(%q): %v", test.constraint, err)
			}
			if got := constraint.Matches(test.tag); got != test.want {
				t.Fatalf("Matches(%q, %q) = %v, want %v", test.constraint, test.tag, got, test.want)
			}
		})
	}
}

func TestHighestMatch(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		tags       []string
		want       string
		wantOK     bool
	}{
		{
			name:       "highest patch within minor",
			constraint: "~1.2",
			tags:       []string{"1.2.0", "1.2.10", "1.2.2", "1.3.0", "latest"},
			want:       "1.2.10",
			wantOK:     true,
		},
		{
			name:       "floating tags ignored",
			constraint: "^1",
			tags:       []string{"1", "1.2", "latest", "1.4.7"},
			want:       "1.4.7",
			wantOK:     true,
		},
		{
			name:       "prereleases excluded",
			constraint: "*",
			tags:       []string{"2.0.0-rc.1", "1.9.0"},
			want:       "1.9.0",
			wantOK:     true,
		},
		{
			name:       "v-prefixed tag preserved",
			constraint: "*",
			tags:       []string{"v1.2.3", "v1.2.4"},
			want:       "v1.2.4",
			wantOK:     true,
		},
		{
			name:       "no match",
			constraint: "~3.0",
			tags:       []string{"1.0.0", "2.0.0"},
			wantOK:     false,
		},
		{
			name:       "empty tag list",
			constraint: "*",
			tags:       nil,
			wantOK:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			constraint, err := ParseConstraint(test.constraint)
			if err != nil {
				t.Fatalf("ParseConstraint(%q): %v", test.constraint, err)
			}
			got, ok := HighestMatch(test.tags, constraint)
			if ok != test.wantOK {
				t.Fatalf("HighestMatch ok = %v, want %v", ok, test.wantOK)
			}
			if got != test.want {
				t.Fatalf("HighestMatch = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompareTags(t *testing.T) {
	tests := []struct {
		name   string
		a      string
		b      string
		want   int
		wantOK bool
	}{
		{name: "newer", a: "1.2.4", b: "1.2.3", want: 1, wantOK: true},
		{name: "equal across v prefix", a: "v1.2.3", b: "1.2.3", want: 0, wantOK: true},
		{name: "older", a: "1.2.3", b: "2.0.0", want: -1, wantOK: true},
		{name: "non-semver left", a: "latest", b: "1.2.3", wantOK: false},
		{name: "floating right", a: "1.2.3", b: "1.2", wantOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := CompareTags(test.a, test.b)
			if ok != test.wantOK {
				t.Fatalf("CompareTags ok = %v, want %v", ok, test.wantOK)
			}
			if ok && got != test.want {
				t.Fatalf("CompareTags = %d, want %d", got, test.want)
			}
		})
	}
}
