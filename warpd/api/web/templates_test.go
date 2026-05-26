package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pangobit/warpgate/warpd/internal/identity"
)

func TestDashboardRendersIdentityInNavigation(t *testing.T) {
	var body bytes.Buffer
	if err := NewRenderer().Render(&body, Dashboard(DashboardPage{
		Title: "Warpgate",
	})); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}

	if !strings.Contains(body.String(), ">unknown<") {
		t.Fatalf("expected navigation identity label in dashboard HTML")
	}
}

func TestIdentityAuthStyleUsesStatusColors(t *testing.T) {
	if identityAuthStyle("unknown").ClassName() != statusWarning().ClassName() {
		t.Fatalf("expected unknown identity to use warning status")
	}
	if identityAuthStyle("ray@example.com").ClassName() != statusSuccess().ClassName() {
		t.Fatalf("expected known identity to use success status")
	}
}

func TestSettingsRendersGitHubConnectionWithoutTokenEnvVar(t *testing.T) {
	var body bytes.Buffer
	if err := NewRenderer().Render(&body, Settings(SettingsPage{
		Title:         "Settings",
		IdentityLabel: "unknown",
		GitHubAuth: identity.GitHubAuthStatus{
			Configured: true,
		},
	})); err != nil {
		t.Fatalf("render settings: %v", err)
	}
	html := body.String()
	if !strings.Contains(html, "Connect GitHub") {
		t.Fatalf("expected GitHub connection action in settings HTML")
	}
	if strings.Contains(html, "OAuth") {
		t.Fatalf("did not expect OAuth app copy in settings HTML")
	}
	if strings.Contains(html, "Token env var") {
		t.Fatalf("did not expect token env var field in settings HTML")
	}
	if !strings.Contains(html, `name="path"`) {
		t.Fatalf("expected repository path field in settings HTML")
	}
}
