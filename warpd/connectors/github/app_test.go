package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testAppKeyPEM(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, keyPEM
}

func TestNewAppTokenProviderValidation(t *testing.T) {
	_, keyPEM := testAppKeyPEM(t)
	tests := []struct {
		name string
		cfg  AppConfig
	}{
		{
			name: "missing app id",
			cfg:  AppConfig{InstallationID: 2, PrivateKeyPEM: keyPEM},
		},
		{
			name: "missing installation id",
			cfg:  AppConfig{AppID: 1, PrivateKeyPEM: keyPEM},
		},
		{
			name: "missing private key",
			cfg:  AppConfig{AppID: 1, InstallationID: 2},
		},
		{
			name: "invalid private key",
			cfg:  AppConfig{AppID: 1, InstallationID: 2, PrivateKeyPEM: []byte("not a key")},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAppTokenProvider(test.cfg); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestAppTokenProviderMintsAndCaches(t *testing.T) {
	key, keyPEM := testAppKeyPEM(t)
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/42/access_tokens" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		verifyAppJWT(t, key, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_test_token",
			"expires_at": now.Add(time.Hour),
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	provider, err := NewAppTokenProvider(AppConfig{AppID: 7, InstallationID: 42, PrivateKeyPEM: keyPEM})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider.apiURL = server.URL
	provider.now = func() time.Time { return now }

	token, err := provider.Token(t.Context())
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	if token != "ghs_test_token" {
		t.Fatalf("token = %q, want ghs_test_token", token)
	}
	if _, err := provider.Token(t.Context()); err != nil {
		t.Fatalf("cached token: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (second call should hit cache)", requests)
	}

	now = now.Add(time.Hour - installationTokenSkew + time.Second)
	if _, err := provider.Token(t.Context()); err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (near-expiry call should re-mint)", requests)
	}
}

func TestAppTokenProviderMintFailure(t *testing.T) {
	_, keyPEM := testAppKeyPEM(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(`{"message":"bad credentials"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	provider, err := NewAppTokenProvider(AppConfig{AppID: 7, InstallationID: 42, PrivateKeyPEM: keyPEM})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider.apiURL = server.URL

	_, err = provider.Token(t.Context())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("error %q should include GitHub's message", err)
	}
}

func verifyAppJWT(t *testing.T, key *rsa.PrivateKey, authorization string) {
	t.Helper()
	raw, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok {
		t.Fatalf("authorization %q is not a bearer token", authorization)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d segments, want 3", len(parts))
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims struct {
		Issuer string `json:"iss"`
		Iat    int64  `json:"iat"`
		Exp    int64  `json:"exp"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Issuer != "7" {
		t.Fatalf("iss = %q, want 7", claims.Issuer)
	}
	if claims.Exp <= claims.Iat {
		t.Fatalf("exp %d must be after iat %d", claims.Exp, claims.Iat)
	}
}
