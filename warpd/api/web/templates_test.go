package web

import (
	"bytes"
	"strings"
	"testing"
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
		GitHubAuth: GitHubAuthView{
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
	if !strings.Contains(html, `name="github_client_id"`) {
		t.Fatalf("expected GitHub App client ID field in settings HTML")
	}
	if !strings.Contains(html, `name="path"`) {
		t.Fatalf("expected repository path field in settings HTML")
	}
}

func TestLogsRenderStructuredRows(t *testing.T) {
	var body bytes.Buffer
	if err := NewRenderer().Render(&body, Logs(LogsPage{
		Title:     "Logs",
		HasResult: true,
		Result: LogsResultView{
			Output: `[api-1] {"level":"info","message":"ready"}` + "\n[worker-1] started\n",
		},
	})); err != nil {
		t.Fatalf("render logs: %v", err)
	}
	html := body.String()
	if !strings.Contains(html, "api-1") || !strings.Contains(html, "ready") {
		t.Fatalf("expected structured api log row in HTML")
	}
	if !strings.Contains(html, "level") || !strings.Contains(html, "message") {
		t.Fatalf("expected pretty JSON fields in HTML")
	}
	if strings.Contains(html, "preBlock") {
		t.Fatalf("did not expect raw preformatted log wall")
	}
}

func TestAppEditRendersEveryReleaseService(t *testing.T) {
	var body bytes.Buffer
	if err := NewRenderer().Render(&body, AppEdit(AppEditPage{
		Title: "Edit api",
		Services: []AppReleaseServiceView{
			{Name: "api", Image: "ghcr.io/acme/api", ImageTag: "v2.0.0"},
			{Name: "worker", Image: "ghcr.io/acme/worker", ImageDigest: "sha256:worker"},
		},
	})); err != nil {
		t.Fatalf("render app edit: %v", err)
	}
	html := body.String()
	if !strings.Contains(html, `name="service" value="api"`) {
		t.Fatalf("expected api service input")
	}
	if !strings.Contains(html, `name="service" value="worker"`) {
		t.Fatalf("expected worker service input")
	}
	if !strings.Contains(html, `value="sha256:worker"`) {
		t.Fatalf("expected worker digest input")
	}
}

func TestReleaseDetailRendersDeployPendingState(t *testing.T) {
	var body bytes.Buffer
	if err := NewRenderer().Render(&body, ReleaseDetail(ReleasePage{
		Title:   "Release rel-1",
		Release: ReleaseView{ID: "rel-1"},
	})); err != nil {
		t.Fatalf("render release detail: %v", err)
	}
	html := body.String()
	if !strings.Contains(html, `src="/assets/js/deploy.js"`) {
		t.Fatalf("expected deploy JavaScript asset")
	}
	if !strings.Contains(html, `data-deploy-form`) || !strings.Contains(html, `data-deploy-button`) {
		t.Fatalf("expected deploy form behavior hooks")
	}
	if !strings.Contains(html, `data-deploy-loading`) || !strings.Contains(html, `Deploying...`) {
		t.Fatalf("expected deploy loading indicator")
	}
}
