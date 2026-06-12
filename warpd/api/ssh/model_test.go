package ssh

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
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
		"api/api",
		"1.2.5",
		"baseline: 1 apps",
		"succeeded",
		"[d] deploy stack",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("view missing %q:\n%s", want, content)
		}
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
