package secrets

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeyToEnvVar(t *testing.T) {
	tests := []struct {
		key    string
		prefix string
		want   string
	}{
		{"auth/prod/session_auth_key", "auth/prod", "SESSION_AUTH_KEY"},
		{"auth/prod/db_url", "auth/prod", "DB_URL"},
		{"auth/prod/nested/deep/key", "auth/prod", "NESTED_DEEP_KEY"},
		{"standalone_key", "", "STANDALONE_KEY"},
		{"auth/prod/key", "auth/prod/", "KEY"},
		{"unmatched/key", "other/prefix", "UNMATCHED_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := KeyToEnvVar(tt.key, tt.prefix)
			if got != tt.want {
				t.Errorf("KeyToEnvVar(%q, %q) = %q, want %q", tt.key, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestFormatDotEnv(t *testing.T) {
	env := map[string]string{
		"DB_URL":     "postgres://localhost/db",
		"API_KEY":    "abc123",
		"WITH_SPACE": "hello world",
	}

	got := FormatDotEnv(env)

	if got == "" {
		t.Fatal("expected non-empty output")
	}

	lines := map[string]bool{}
	for _, line := range splitLines(got) {
		if line != "" {
			lines[line] = true
		}
	}

	if !lines["API_KEY=abc123"] {
		t.Error("missing API_KEY=abc123")
	}
	if !lines["DB_URL=postgres://localhost/db"] {
		t.Error("missing DB_URL line")
	}
	if !lines["WITH_SPACE='hello world'"] {
		t.Error("missing quoted WITH_SPACE line")
	}
}

func TestFormatDotEnvQuoting(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"simple", "abc123", "KEY=abc123\n"},
		{"space", "hello world", "KEY='hello world'\n"},
		{"dollar", "price$5", "KEY='price$5'\n"},
		{"single_quote", "it's", "KEY='it'\\''s'\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDotEnv(map[string]string{"KEY": tt.value})
			if got != tt.want {
				t.Errorf("FormatDotEnv({KEY: %q}) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestFetchEnv(t *testing.T) {
	secrets := map[string]string{
		"myapp/prod/db_url":   "postgres://localhost/db",
		"myapp/prod/api_key":  "secret123",
		"myapp/prod/disabled": "nope",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/secrets" && r.URL.Query().Get("prefix") == "myapp/prod":
			entries := []map[string]any{
				{"key": "myapp/prod/db_url", "state": "active"},
				{"key": "myapp/prod/api_key", "state": "active"},
				{"key": "myapp/prod/disabled", "state": "disabled"},
			}
			json.NewEncoder(w).Encode(entries)
		case r.URL.RawPath == "/api/secrets/myapp%2Fprod%2Fdb_url" || r.URL.Path == "/api/secrets/myapp/prod/db_url":
			json.NewEncoder(w).Encode(map[string]string{
				"key": "myapp/prod/db_url", "value": secrets["myapp/prod/db_url"], "state": "active",
			})
		case r.URL.RawPath == "/api/secrets/myapp%2Fprod%2Fapi_key" || r.URL.Path == "/api/secrets/myapp/prod/api_key":
			json.NewEncoder(w).Encode(map[string]string{
				"key": "myapp/prod/api_key", "value": secrets["myapp/prod/api_key"], "state": "active",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	env, err := client.FetchEnv("myapp/prod")
	if err != nil {
		t.Fatalf("FetchEnv() error: %v", err)
	}

	if len(env) != 2 {
		t.Fatalf("expected 2 env vars, got %d: %v", len(env), env)
	}
	if env["DB_URL"] != "postgres://localhost/db" {
		t.Errorf("DB_URL = %q, want postgres://localhost/db", env["DB_URL"])
	}
	if env["API_KEY"] != "secret123" {
		t.Errorf("API_KEY = %q, want secret123", env["API_KEY"])
	}
}

func TestFetchEnvSealedVault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"error": "vault is sealed"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchEnv("myapp/prod")
	if err == nil {
		t.Fatal("expected error for sealed vault")
	}
}

func TestFetchEnvEmptyPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/secrets" {
			json.NewEncoder(w).Encode([]map[string]any{})
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	env, err := client.FetchEnv("")
	if err != nil {
		t.Fatalf("FetchEnv() error: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("expected empty map, got %v", env)
	}
}

func TestSetSecret(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"success", http.StatusCreated, false},
		{"sealed", http.StatusServiceUnavailable, true},
		{"server_error", http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotKey, gotValue string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				r.ParseForm()
				gotKey = r.URL.Path[len("/api/secrets/"):]
				gotValue = r.FormValue("value")
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			client := NewClient(srv.URL)
			err := client.SetSecret("warpgate/registry/token", "ghp_abc123")

			if (err != nil) != tt.wantErr {
				t.Errorf("SetSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if gotKey != "warpgate/registry/token" {
					t.Errorf("key = %q, want warpgate/registry/token", gotKey)
				}
				if gotValue != "ghp_abc123" {
					t.Errorf("value = %q, want ghp_abc123", gotValue)
				}
			}
		})
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
