package ssh

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/audit"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/stackstate"
	"github.com/pangobit/warpgate/warpd/usecase"
)

func testOperator() identity.User {
	return identity.User{
		Email:        "ray@ssh",
		DisplayName:  "ray",
		Capabilities: []string{identity.AdminCapability},
	}
}

func testOverview() overview {
	finished := time.Now().Add(-time.Minute)
	return overview{
		repo:     configrepo.RepositorySettings{Owner: "acme", Repo: "infra", Branch: "main"},
		attached: true,
		cursor:   configrepo.SyncCursor{LastObservedCommit: "abcdef1234567890", LastCheckedAt: time.Now()},
		cursors: []imagewatch.Cursor{{
			App:          "api",
			Service:      "api",
			Tag:          "1.2.0",
			CandidateTag: "1.2.5",
			Status:       imagewatch.StatusUpdateAvailable,
		}},
		stack: stackstate.State{
			LastHealthy: stackstate.Snapshot{
				Releases:  map[string]string{"api": "rel-api-1"},
				UpdatedAt: time.Now().Add(-time.Hour),
			},
			LastAttempt: &stackstate.Attempt{
				ID:         "stack-1",
				Status:     stackstate.StatusSucceeded,
				ActorEmail: "ray@ssh",
				StartedAt:  time.Now().Add(-2 * time.Minute),
				FinishedAt: &finished,
			},
		},
		apps: []configrepo.AppSnapshot{{Name: "api"}},
		appServices: []usecase.AppReleaseServices{{
			Name: "api",
			Services: []usecase.AppReleaseService{
				{Name: "api", Image: "ghcr.io/acme/api", ImageTag: "1.2.0"},
				{Name: "worker", Image: "ghcr.io/acme/worker", ImageTag: "1.1.0"},
			},
		}},
		baselineReleases: []usecase.AppBaselineRelease{{
			Name:            "api",
			ConfigCommit:    "abcdef1234567890",
			PrimaryImageTag: "v1.2.0",
			Services: []usecase.AppDeployedService{
				{Name: "api", ImageRef: "ghcr.io/acme/api:v1.2.0"},
				{Name: "worker", ImageRef: "ghcr.io/acme/worker:v1.1.0"},
			},
		}},
	}
}

func newTestModel() model {
	service := usecase.NewService(tursoconn.NewMemoryStore(), nil, nil, nil)
	return newModel(service, testOperator(), func() {})
}

func updated(t *testing.T, m tea.Model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	result, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", next)
	}
	return result
}

func TestDashboardRendersOverviewSections(t *testing.T) {
	m := updated(t, newTestModel(), overviewMsg{data: testOverview()})
	content := m.View().Content
	for _, want := range []string{
		"acme/infra@main",
		"abcdef12",
		"1 semver update pending — see Apps section",
		"→ 1.2.5 (commit pending)",
		"baseline: 1 apps",
		"succeeded",
		"commit abcdef12 · v1.2.0",
		"ghcr.io/acme/api:v1.2.0",
		"ghcr.io/acme/worker:v1.1.0",
		"[d] deploy stack",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("view missing %q:\n%s", want, content)
		}
	}
}

func TestAppsSectionShowsMissingBaselineRelease(t *testing.T) {
	data := testOverview()
	data.baselineReleases = []usecase.AppBaselineRelease{{
		Name:           "api",
		ReleaseMissing: true,
	}}
	m := updated(t, newTestModel(), overviewMsg{data: data})
	content := m.View().Content
	if !strings.Contains(content, "release record missing") {
		t.Fatalf("expected missing release error:\n%s", content)
	}
}

func TestAppsSectionShowsInvalidCursorInline(t *testing.T) {
	data := testOverview()
	data.cursors = []imagewatch.Cursor{{
		App:       "api",
		Service:   "api",
		Status:    imagewatch.StatusInvalid,
		LastError: "registry unreachable",
	}}
	m := updated(t, newTestModel(), overviewMsg{data: data})
	content := m.View().Content
	if !strings.Contains(content, "registry unreachable") {
		t.Fatalf("expected invalid cursor error inline:\n%s", content)
	}
}

func TestAppsSectionLongErrorsUseDetailsPanel(t *testing.T) {
	data := testOverview()
	imageWatchError := strings.Repeat("registry rejected token ", 6)
	configError := strings.Repeat("compose parse failed ", 6)
	manifestError := strings.Repeat("release manifest missing service ", 5)
	data.apps = []configrepo.AppSnapshot{{Name: "api"}, {Name: "worker"}}
	data.stack.LastHealthy.Releases = map[string]string{"api": "rel-api-1"}
	data.cursors = []imagewatch.Cursor{{
		App:       "api",
		Service:   "api",
		Status:    imagewatch.StatusInvalid,
		LastError: imageWatchError,
	}}
	data.appServices = []usecase.AppReleaseServices{{
		Name:       "worker",
		ParseError: configError,
	}}
	data.baselineReleases = []usecase.AppBaselineRelease{{
		Name:          "api",
		ManifestError: manifestError,
	}}

	m := updated(t, newTestModel(), tea.WindowSizeMsg{Width: 58, Height: 16})
	m = updated(t, m, overviewMsg{data: data})

	content := m.View().Content
	for _, want := range []string{
		"Details",
		"manifest error:",
		"config error:",
		"...",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("view missing %q:\n%s", want, content)
		}
	}
	details := m.detailContent()
	for _, want := range []string{imageWatchError, configError, manifestError} {
		if !strings.Contains(details, want) {
			t.Fatalf("details missing full error %q:\n%s", want, details)
		}
	}
}

func TestUpdatesSectionSummarizesPending(t *testing.T) {
	m := updated(t, newTestModel(), overviewMsg{data: testOverview()})
	content := m.View().Content
	if !strings.Contains(content, "1 semver update pending — see Apps section") {
		t.Fatalf("expected pending update summary:\n%s", content)
	}
	if strings.Contains(content, "api/api              1.2.0") {
		t.Fatalf("expected pending updates section to avoid per-service duplicate lines:\n%s", content)
	}
}

func TestAppsSectionShowsNotDeployedLabel(t *testing.T) {
	data := testOverview()
	data.stack.LastHealthy.Releases = map[string]string{}
	data.baselineReleases = nil
	data.apps = []configrepo.AppSnapshot{{Name: "api"}, {Name: "worker"}}
	data.appServices = []usecase.AppReleaseServices{
		{
			Name: "api",
			Services: []usecase.AppReleaseService{
				{Name: "api", Image: "ghcr.io/acme/api", ImageTag: "1.2.0"},
			},
		},
		{
			Name: "worker",
			Services: []usecase.AppReleaseService{
				{Name: "worker", Image: "ghcr.io/acme/worker", ImageTag: "1.0.0"},
			},
		},
	}
	m := updated(t, newTestModel(), overviewMsg{data: data})
	content := m.View().Content
	if !strings.Contains(content, "not in baseline") {
		t.Fatalf("expected not in baseline label:\n%s", content)
	}
	if !strings.Contains(content, "ghcr.io/acme/worker:1.0.0 (not deployed)") {
		t.Fatalf("expected not deployed label on desired service:\n%s", content)
	}
}

func TestAppsSectionRendersParseError(t *testing.T) {
	data := testOverview()
	data.stack.LastHealthy.Releases = map[string]string{}
	data.baselineReleases = nil
	data.appServices = []usecase.AppReleaseServices{{
		Name:       "api",
		ParseError: "yaml: unmarshal errors",
	}}
	m := updated(t, newTestModel(), overviewMsg{data: data})
	content := m.View().Content
	if !strings.Contains(content, "config error: yaml: unmarshal errors") {
		t.Fatalf("expected parse error in apps section:\n%s", content)
	}
}

func TestDeployRequiresConfirmation(t *testing.T) {
	m := updated(t, newTestModel(), overviewMsg{data: testOverview()})
	m = updated(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if !strings.Contains(m.View().Content, "Deploy the stack now? [y/n]") {
		t.Fatalf("expected confirmation prompt:\n%s", m.View().Content)
	}
	m = updated(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if strings.Contains(m.View().Content, "[y/n]") {
		t.Fatal("confirmation prompt should clear after 'n'")
	}
	if m.busy {
		t.Fatal("declined confirmation must not start a deploy")
	}
}

func TestConfirmedDeployBlocksQuitWhileBusy(t *testing.T) {
	m := updated(t, newTestModel(), overviewMsg{data: testOverview()})
	m = updated(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = next.(model)
	if !m.busy {
		t.Fatal("confirmed deploy should mark the model busy")
	}
	if cmd == nil {
		t.Fatal("confirmed deploy should return a command")
	}
	quitNext, quitCmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if quitCmd != nil {
		t.Fatal("quit must be disabled while an operation runs")
	}
	if quitNext.(model).busy != true {
		t.Fatal("busy state must persist while the operation runs")
	}
}

func TestOpDoneShowsResultAndReloads(t *testing.T) {
	m := updated(t, newTestModel(), overviewMsg{data: testOverview()})
	m.busy = true
	next, cmd := m.Update(opDoneMsg{label: "stack deploy", attempt: stackstate.Attempt{Status: stackstate.StatusReverted}})
	m = next.(model)
	if m.busy {
		t.Fatal("op completion must clear busy state")
	}
	if !strings.Contains(m.View().Content, "stack deploy finished: reverted") {
		t.Fatalf("expected completion notice:\n%s", m.View().Content)
	}
	if cmd == nil {
		t.Fatal("op completion should reload the overview")
	}
}

func TestDashboardLongErrorsUseDetailsPanel(t *testing.T) {
	data := testOverview()
	longError := strings.Repeat("config sync failed at validation step ", 8)
	data.cursor.LastError = longError
	data.stack.LastAttempt.Error = strings.Repeat("deploy log line\n", 12)
	data.stack.LastAttempt.FailedApp = "api"

	m := updated(t, newTestModel(), tea.WindowSizeMsg{Width: 60, Height: 16})
	m = updated(t, m, overviewMsg{data: data})

	content := m.View().Content
	for _, want := range []string{
		"Details",
		"scroll details",
		"config sync failed at validation step",
		"...",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("view missing %q:\n%s", want, content)
		}
	}
	if !strings.Contains(m.detailContent(), longError) {
		t.Fatal("detail panel content must retain the full sync error")
	}
}

func TestDashboardDetailsCanScrollWithoutTakingDeployKey(t *testing.T) {
	data := testOverview()
	data.cursor.LastError = strings.Repeat("sync line\n", 20)

	m := updated(t, newTestModel(), tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated(t, m, overviewMsg{data: data})
	before := m.panel.YOffset()

	m = updated(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.panel.YOffset() <= before {
		t.Fatalf("expected details panel to scroll from %d, got %d", before, m.panel.YOffset())
	}

	m = updated(t, m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.confirm != confirmDeploy {
		t.Fatal("deploy key should still arm deployment on the dashboard")
	}
}

func TestAuditViewUsesScrollablePanel(t *testing.T) {
	events := make([]audit.Event, 20)
	for i := range events {
		events[i] = audit.Event{
			CreatedAt:  time.Date(2026, time.January, 2, 3, i, 0, 0, time.UTC),
			Type:       "deploy",
			ActorEmail: "ray@ssh",
			Message:    strings.Repeat("event message ", 6),
		}
	}

	m := updated(t, newTestModel(), tea.WindowSizeMsg{Width: 72, Height: 12})
	m.view = viewAudit
	m = updated(t, m, auditMsg{events: events})
	content := m.View().Content
	if !strings.Contains(content, "Audit log") || !strings.Contains(content, "pgup/pgdn") {
		t.Fatalf("audit view missing panel chrome:\n%s", content)
	}

	before := m.panel.YOffset()
	m = updated(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.panel.YOffset() <= before {
		t.Fatalf("expected audit panel to scroll from %d, got %d", before, m.panel.YOffset())
	}
}

func TestLoadErrorSummaryKeepsFullDetails(t *testing.T) {
	longError := strings.Repeat("repository unavailable ", 8)

	m := updated(t, newTestModel(), tea.WindowSizeMsg{Width: 48, Height: 12})
	m = updated(t, m, loadErrMsg{err: errors.New(longError)})

	content := m.View().Content
	if !strings.Contains(content, "Error: repository unavailable") || !strings.Contains(content, "...") {
		t.Fatalf("load error should be summarized:\n%s", content)
	}
	if !strings.Contains(m.detailContent(), longError) {
		t.Fatal("detail content must retain the full load error")
	}
}
