package ssh

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pangobit/warpgate/warpd/internal/audit"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/stackstate"
	"github.com/pangobit/warpgate/warpd/usecase"
)

const (
	viewDashboard = "dashboard"
	viewAudit     = "audit"

	confirmDeploy   = "deploy"
	confirmRollback = "rollback"

	opTimeout      = 30 * time.Minute
	loadTimeout    = 30 * time.Second
	auditPageLimit = 50
)

type overview struct {
	repo     configrepo.RepositorySettings
	attached bool
	cursor   configrepo.SyncCursor
	cursors  []imagewatch.Cursor
	stack    stackstate.State
	apps     []configrepo.AppSnapshot
}

type overviewMsg struct {
	data overview
}

type auditMsg struct {
	events []audit.Event
}

type opDoneMsg struct {
	label   string
	attempt stackstate.Attempt
	err     error
}

type loadErrMsg struct {
	err error
}

type model struct {
	service *usecase.Service
	actor   identity.User
	refresh func()

	view     string
	confirm  string
	busy     bool
	busyWhat string
	spinner  spinner.Model
	data     *overview
	audit    []audit.Event
	notice   string
	loadErr  error
}

func newModel(service *usecase.Service, actor identity.User, refresh func()) model {
	return model{
		service: service,
		actor:   actor,
		refresh: refresh,
		view:    viewDashboard,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

// Init loads the first overview snapshot.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadOverview())
}

// Update handles TUI messages.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case overviewMsg:
		data := msg.data
		m.data = &data
		m.loadErr = nil
		return m, nil
	case auditMsg:
		m.audit = msg.events
		m.loadErr = nil
		return m, nil
	case loadErrMsg:
		m.loadErr = msg.err
		return m, nil
	case opDoneMsg:
		m.busy = false
		m.busyWhat = ""
		if msg.err != nil {
			m.notice = msg.label + " failed: " + msg.err.Error()
		} else {
			m.notice = msg.label + " finished: " + string(msg.attempt.Status)
		}
		return m, m.loadOverview()
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.confirm != "" {
		return m.handleConfirmKey(key)
	}
	switch key {
	case "q", "ctrl+c":
		if !m.busy {
			return m, tea.Quit
		}
		return m, nil
	case "d":
		return m.armConfirm(confirmDeploy), nil
	case "r":
		return m.armConfirm(confirmRollback), nil
	default:
		return m.handleViewKey(key)
	}
}

func (m model) armConfirm(action string) model {
	if !m.busy && m.view == viewDashboard {
		m.confirm = action
	}
	return m
}

func (m model) handleViewKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "s":
		if m.refresh != nil {
			m.refresh()
			m.notice = "daemon refresh scheduled"
		}
		return m, m.loadOverview()
	case "u":
		return m, m.loadOverview()
	case "a":
		if m.view == viewAudit {
			m.view = viewDashboard
			return m, nil
		}
		m.view = viewAudit
		return m, m.loadAudit()
	case "esc":
		m.view = viewDashboard
	}
	return m, nil
}

func (m model) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	action := m.confirm
	switch key {
	case "y":
		m.confirm = ""
		m.busy = true
		m.notice = ""
		if action == confirmDeploy {
			m.busyWhat = "deploying stack"
			return m, tea.Batch(m.spinner.Tick, m.deployStack())
		}
		m.busyWhat = "rolling back to last healthy baseline"
		return m, tea.Batch(m.spinner.Tick, m.rollbackStack())
	case "n", "esc", "q":
		m.confirm = ""
	}
	return m, nil
}

// View renders the operator TUI.
func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString(m.header())
	if m.loadErr != nil {
		b.WriteString(errStyle.Render("Error: "+m.loadErr.Error()) + "\n\n")
	}
	if m.notice != "" {
		b.WriteString(noticeStyle.Render(m.notice) + "\n\n")
	}
	switch {
	case m.busy:
		b.WriteString("  " + m.spinner.View() + " " + m.busyWhat + "...\n")
	case m.view == viewAudit:
		b.WriteString(m.auditView())
	default:
		b.WriteString(m.dashboardView())
	}
	b.WriteString("\n")
	b.WriteString(m.footer())
	view := tea.NewView(b.String())
	view.AltScreen = true
	return view
}

func (m model) header() string {
	title := titleStyle.Render("Warpgate")
	if m.data != nil && m.data.attached {
		repo := m.data.repo.Owner + "/" + m.data.repo.Repo + "@" + m.data.repo.Branch
		if m.data.repo.Path != "" {
			repo += " (" + m.data.repo.Path + ")"
		}
		title += dimStyle.Render(" — " + repo)
	}
	return title + "\n\n"
}

func (m model) footer() string {
	if m.confirm == confirmDeploy {
		return confirmStyle.Render("Deploy the stack now? [y/n]") + "\n"
	}
	if m.confirm == confirmRollback {
		return confirmStyle.Render("Roll back to the last healthy baseline? [y/n]") + "\n"
	}
	if m.busy {
		return dimStyle.Render("  operation in progress — quitting is disabled") + "\n"
	}
	if m.view == viewAudit {
		return dimStyle.Render("  [a/esc] back  [u] reload  [q] quit") + "\n"
	}
	return dimStyle.Render("  [d] deploy stack  [r] rollback  [s] sync now  [a] audit  [u] reload  [q] quit") + "\n"
}

func (m model) dashboardView() string {
	if m.data == nil {
		return "  " + m.spinner.View() + " loading...\n"
	}
	var b strings.Builder
	b.WriteString(m.configSection())
	b.WriteString(m.stackSection())
	b.WriteString(m.updatesSection())
	b.WriteString(m.appsSection())
	return b.String()
}

func (m model) configSection() string {
	if !m.data.attached {
		return errStyle.Render("No repository attached. Set WARPGATE_REPO and restart the daemon.") + "\n\n"
	}
	line := "  commit " + shortSHA(m.data.cursor.LastObservedCommit) + "  checked " + relativeTime(m.data.cursor.LastCheckedAt)
	if m.data.cursor.LastError != "" {
		line += "\n  " + errStyle.Render("sync error: "+m.data.cursor.LastError)
	}
	return headerStyle.Render("Config") + "\n" + line + "\n\n"
}

func (m model) stackSection() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Stack") + "\n")
	stack := m.data.stack
	if len(stack.LastHealthy.Releases) == 0 {
		b.WriteString("  baseline: " + dimStyle.Render("none — first successful stack deploy records it") + "\n")
	} else {
		b.WriteString(fmt.Sprintf("  baseline: %d apps, advanced %s\n", len(stack.LastHealthy.Releases), relativeTime(stack.LastHealthy.UpdatedAt)))
	}
	if stack.LastAttempt != nil {
		attempt := stack.LastAttempt
		line := "  last attempt: " + styleAttemptStatus(attempt.Status) + dimStyle.Render("  by "+attempt.ActorEmail+" "+relativeTime(attempt.StartedAt))
		b.WriteString(line + "\n")
		if attempt.FailedApp != "" {
			b.WriteString("    " + errStyle.Render("failed app: "+attempt.FailedApp+" — "+attempt.Error) + "\n")
		}
		if attempt.RevertError != "" {
			b.WriteString("    " + errStyle.Render("REVERT FAILED — operator attention required: "+attempt.RevertError) + "\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

func (m model) updatesSection() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Pending updates") + "\n")
	pending := 0
	for _, cursor := range m.data.cursors {
		switch cursor.Status {
		case imagewatch.StatusUpdateAvailable:
			pending++
			b.WriteString(fmt.Sprintf("  %-20s %-12s %s → %s\n", cursor.App+"/"+cursor.Service, cursor.Tag, cursor.CandidateTag, dimStyle.Render("(commit pending)")))
		case imagewatch.StatusInvalid:
			pending++
			b.WriteString("  " + errStyle.Render(fmt.Sprintf("%-20s %s", cursor.App+"/"+cursor.Service, cursor.LastError)) + "\n")
		}
	}
	if pending == 0 {
		b.WriteString("  " + dimStyle.Render("none — stack matches the registry") + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func (m model) appsSection() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("Apps (%d)", len(m.data.apps))) + "\n")
	stack := m.data.stack
	for _, app := range m.data.apps {
		release := stack.LastHealthy.Releases[app.Name]
		if release == "" {
			release = dimStyle.Render("not in baseline")
		}
		b.WriteString(fmt.Sprintf("  %-24s %s\n", app.Name, release))
	}
	if len(m.data.apps) == 0 {
		b.WriteString("  " + dimStyle.Render("no apps synced") + "\n")
	}
	return b.String()
}

func (m model) auditView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Audit log") + "\n")
	for _, event := range m.audit {
		b.WriteString(fmt.Sprintf("  %s  %-24s %-20s %s\n",
			event.CreatedAt.Format("01-02 15:04"), event.Type, event.ActorEmail, event.Message))
	}
	if len(m.audit) == 0 {
		b.WriteString("  " + dimStyle.Render("no events") + "\n")
	}
	return b.String()
}

func (m model) loadOverview() tea.Cmd {
	service := m.service
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		var data overview
		var err error
		data.repo, data.attached, err = service.RepositorySettings(ctx)
		if err != nil {
			return loadErrMsg{err: err}
		}
		dashboard, err := service.Dashboard(ctx)
		if err != nil {
			return loadErrMsg{err: err}
		}
		data.cursor = dashboard.ConfigCursor
		data.cursors, err = service.ImageCursors(ctx)
		if err != nil {
			return loadErrMsg{err: err}
		}
		data.stack, err = service.StackState(ctx)
		if err != nil {
			return loadErrMsg{err: err}
		}
		data.apps, err = service.Apps(ctx)
		if err != nil {
			return loadErrMsg{err: err}
		}
		return overviewMsg{data: data}
	}
}

func (m model) loadAudit() tea.Cmd {
	service := m.service
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		events, err := service.AuditEvents(ctx, auditPageLimit)
		if err != nil {
			return loadErrMsg{err: err}
		}
		return auditMsg{events: events}
	}
}

func (m model) deployStack() tea.Cmd {
	service := m.service
	actor := m.actor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		attempt, err := service.DeployStack(ctx, actor)
		return opDoneMsg{label: "stack deploy", attempt: attempt, err: err}
	}
}

func (m model) rollbackStack() tea.Cmd {
	service := m.service
	actor := m.actor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		attempt, err := service.RollbackStack(ctx, actor)
		return opDoneMsg{label: "stack rollback", attempt: attempt, err: err}
	}
}

func styleAttemptStatus(status stackstate.Status) string {
	switch status {
	case stackstate.StatusSucceeded:
		return okStyle.Render(string(status))
	case stackstate.StatusRunning:
		return noticeStyle.Render(string(status))
	default:
		return errStyle.Render(string(status))
	}
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "-"
	}
	return sha
}

func relativeTime(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	elapsed := time.Since(at).Truncate(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	return elapsed.String() + " ago"
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#606060"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4672"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	noticeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
	confirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD700"))
)
