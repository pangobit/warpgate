package ssh

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
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
	defaultWidth   = 100
	defaultHeight  = 32
	minPanelHeight = 4

	terminalSideMargin = 2
	ellipsisReserve    = 3

	loadErrorPrefix         = "Error: "
	syncErrorPrefix         = "  sync error: "
	failedAppLinePrefix     = "    failed app: "
	failedAppLineSeparator  = " — "
	revertFailedPrefix      = "    REVERT FAILED — operator attention required: "
	configErrorPrefix       = "    config error: "
	manifestErrorPrefix     = "manifest error: "
	invalidStatusPrefix     = " — "
	pendingUpdateLineIndent = 2
	pendingUpdateNameWidth  = 20

	appRowIndent           = 2
	appNameColumnWidth     = 24
	serviceRowIndent       = 4
	serviceNameColumnWidth = 22
	serviceImageSeparator  = " "
)

type overview struct {
	repo             configrepo.RepositorySettings
	attached         bool
	cursor           configrepo.SyncCursor
	cursors          []imagewatch.Cursor
	stack            stackstate.State
	apps             []configrepo.AppSnapshot
	appServices      []usecase.AppReleaseServices
	baselineReleases []usecase.AppBaselineRelease
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

	view            string
	confirm         string
	busy            bool
	busyWhat        string
	spinner         spinner.Model
	data            *overview
	audit           []audit.Event
	notice          string
	loadErr         error
	width           int
	height          int
	compact         bool
	scrollDashboard bool
	panel           viewport.Model
	bodyPanel       viewport.Model
}

func newModel(service *usecase.Service, actor identity.User, refresh func()) model {
	panel := viewport.New(viewport.WithWidth(defaultWidth), viewport.WithHeight(defaultHeight))
	panel.SoftWrap = true
	bodyPanel := viewport.New(viewport.WithWidth(defaultWidth), viewport.WithHeight(defaultHeight))
	bodyPanel.SoftWrap = true
	return model{
		service:   service,
		actor:     actor,
		refresh:   refresh,
		view:      viewDashboard,
		spinner:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		width:     defaultWidth,
		height:    defaultHeight,
		panel:     panel,
		bodyPanel: bodyPanel,
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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updatePanelContent()
		m.resizePanel()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case overviewMsg:
		data := msg.data
		m.data = &data
		m.loadErr = nil
		m.updatePanelContent()
		m.resizePanel()
		return m, nil
	case auditMsg:
		m.audit = msg.events
		m.loadErr = nil
		m.updatePanelContent()
		m.resizePanel()
		return m, nil
	case loadErrMsg:
		m.loadErr = msg.err
		m.updatePanelContent()
		m.resizePanel()
		return m, nil
	case opDoneMsg:
		m.busy = false
		m.busyWhat = ""
		if msg.err != nil {
			m.notice = msg.label + " failed: " + msg.err.Error()
		} else {
			m.notice = msg.label + " finished: " + string(msg.attempt.Status)
		}
		m.updatePanelContent()
		m.resizePanel()
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
		return m.handleViewKey(msg)
	}
}

func (m model) armConfirm(action string) model {
	if !m.busy && m.view == viewDashboard {
		m.confirm = action
	}
	return m
}

func (m model) handleViewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
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
			m.updatePanelContent()
			m.resizePanel()
			return m, nil
		}
		m.view = viewAudit
		m.updatePanelContent()
		m.resizePanel()
		return m, m.loadAudit()
	case "esc":
		m.view = viewDashboard
		m.updatePanelContent()
		m.resizePanel()
	}
	if m.scrollPanelActive() {
		var cmd tea.Cmd
		if m.scrollDashboard {
			m.bodyPanel, cmd = m.bodyPanel.Update(msg)
			return m, cmd
		}
		m.panel, cmd = m.panel.Update(msg)
		return m, cmd
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
	b.WriteString(m.loadErrorBlock())
	b.WriteString(m.noticeBlock())
	switch {
	case m.busy:
		b.WriteString("  " + m.spinner.View() + " " + m.busyWhat + "...\n")
	case m.scrollDashboard:
		b.WriteString(m.bodyPanel.View())
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
		return dimStyle.Render("  [j/k] scroll  [pgup/pgdn] page  [a/esc] back  [u] reload  [q] quit") + "\n"
	}
	if m.scrollDashboard || m.hasDetailPanel() {
		return dimStyle.Render("  [j/k] scroll details  [d] deploy stack  [r] rollback  [s] sync now  [a] audit  [u] reload  [q] quit") + "\n"
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
		line += "\n" + errStyle.Render(syncErrorPrefix+m.shortText(m.data.cursor.LastError, m.contentWidth()-len(syncErrorPrefix)))
	}
	return headerStyle.Render("Config") + "\n" + line + "\n\n"
}

func (m model) stackSection() string {
	var b strings.Builder
	b.WriteString(m.stackSummary())
	if m.hasDetailPanel() && !m.scrollDashboard {
		b.WriteString(m.detailPanelHeader())
		b.WriteString(m.panel.View())
		return b.String() + "\n"
	}
	b.WriteString("\n")
	return b.String()
}

func (m model) stackSummary() string {
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
		if !m.compact {
			if attempt.FailedApp != "" {
				failedAppWidth := m.contentWidth() - len(failedAppLinePrefix) - len(failedAppLineSeparator) - len(attempt.FailedApp)
				b.WriteString(errStyle.Render(failedAppLinePrefix+attempt.FailedApp+failedAppLineSeparator+m.shortText(attempt.Error, failedAppWidth)) + "\n")
			}
			if attempt.RevertError != "" {
				b.WriteString(errStyle.Render(revertFailedPrefix+m.shortText(attempt.RevertError, m.contentWidth()-len(revertFailedPrefix))) + "\n")
			}
		}
	}
	return b.String()
}

func (m model) updatesSection() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Pending updates") + "\n")
	updatePending := 0
	invalid := 0
	for _, cursor := range m.data.cursors {
		switch cursor.Status {
		case imagewatch.StatusUpdateAvailable:
			updatePending++
		case imagewatch.StatusInvalid:
			invalid++
			if !m.compact {
				prefix := fmt.Sprintf("%-*s ", pendingUpdateNameWidth, cursor.App+"/"+cursor.Service)
				linePrefix := strings.Repeat(" ", pendingUpdateLineIndent) + prefix
				b.WriteString(errStyle.Render(linePrefix+m.shortText(cursor.LastError, m.contentWidth()-len(linePrefix))) + "\n")
			}
		}
	}
	if invalid == 1 {
		b.WriteString("  " + dimStyle.Render("1 invalid image constraint — see Details") + "\n")
	} else if invalid > 1 {
		b.WriteString("  " + dimStyle.Render(fmt.Sprintf("%d invalid image constraints — see Details", invalid)) + "\n")
	}
	if updatePending == 1 {
		b.WriteString("  " + dimStyle.Render("1 semver update pending — see Apps section") + "\n")
	} else if updatePending > 1 {
		b.WriteString("  " + dimStyle.Render(fmt.Sprintf("%d semver updates pending — see Apps section", updatePending)) + "\n")
	}
	if updatePending == 0 && invalid == 0 {
		b.WriteString("  " + dimStyle.Render("none — stack matches the registry") + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

type appsSectionLookups struct {
	servicesByApp       map[string]usecase.AppReleaseServices
	cursorsByAppService map[string]imagewatch.Cursor
	baselineByApp       map[string]usecase.AppBaselineRelease
}

type appServiceRow struct {
	Name     string
	ImageRef string
}

func (m model) appsSectionLookups() appsSectionLookups {
	lookups := appsSectionLookups{
		servicesByApp:       make(map[string]usecase.AppReleaseServices, len(m.data.appServices)),
		cursorsByAppService: make(map[string]imagewatch.Cursor, len(m.data.cursors)),
		baselineByApp:       make(map[string]usecase.AppBaselineRelease, len(m.data.baselineReleases)),
	}
	for _, entry := range m.data.appServices {
		lookups.servicesByApp[entry.Name] = entry
	}
	for _, cursor := range m.data.cursors {
		lookups.cursorsByAppService[cursor.App+"/"+cursor.Service] = cursor
	}
	for _, entry := range m.data.baselineReleases {
		lookups.baselineByApp[entry.Name] = entry
	}
	return lookups
}

func (m model) appsSection() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("Apps (%d)", len(m.data.apps))) + "\n")
	if len(m.data.apps) == 0 {
		b.WriteString("  " + dimStyle.Render("no apps synced") + "\n")
		return b.String()
	}
	if m.compact {
		b.WriteString("  " + dimStyle.Render("collapsed — scroll Details for per-app output") + "\n")
		return b.String() + "\n"
	}
	lookups := m.appsSectionLookups()
	for _, app := range m.data.apps {
		if m.data.stack.LastHealthy.Releases[app.Name] != "" {
			m.writeBaselineApp(&b, app.Name, lookups.baselineByApp[app.Name], lookups)
			continue
		}
		m.writeDesiredApp(&b, app.Name, lookups.servicesByApp[app.Name], lookups)
	}
	return b.String()
}

func (m model) formatBaselineAppLabel(baseline usecase.AppBaselineRelease) string {
	if baseline.ReleaseMissing {
		return errStyle.Render("release record missing")
	}
	if baseline.ManifestError != "" {
		manifestWidth := m.contentWidth() - appRowIndent - appNameColumnWidth - len(manifestErrorPrefix)
		return errStyle.Render(manifestErrorPrefix + m.shortText(baseline.ManifestError, manifestWidth))
	}
	return usecase.BaselineReleaseLabel(baseline)
}

func (m model) writeBaselineApp(b *strings.Builder, appName string, baseline usecase.AppBaselineRelease, lookups appsSectionLookups) {
	b.WriteString(fmt.Sprintf("  %-*s %s\n", appNameColumnWidth, appName, m.formatBaselineAppLabel(baseline)))
	if baseline.ReleaseMissing || baseline.ManifestError != "" {
		return
	}
	if len(baseline.Services) == 0 {
		b.WriteString("    " + dimStyle.Render("no release services") + "\n")
		return
	}
	rows := make([]appServiceRow, len(baseline.Services))
	for i, service := range baseline.Services {
		rows[i] = appServiceRow{Name: service.Name, ImageRef: service.ImageRef}
	}
	m.writeAppServiceRows(b, appName, rows, lookups)
}

func (m model) writeDesiredApp(b *strings.Builder, appName string, entry usecase.AppReleaseServices, lookups appsSectionLookups) {
	b.WriteString(fmt.Sprintf("  %-*s %s\n", appNameColumnWidth, appName, dimStyle.Render("not in baseline")))
	if entry.Name == "" {
		b.WriteString("    " + dimStyle.Render("no release services") + "\n")
		return
	}
	if entry.ParseError != "" {
		b.WriteString(errStyle.Render(configErrorPrefix+m.shortText(entry.ParseError, m.contentWidth()-len(configErrorPrefix))) + "\n")
		return
	}
	if len(entry.Services) == 0 {
		b.WriteString("    " + dimStyle.Render("no release services") + "\n")
		return
	}
	rows := make([]appServiceRow, len(entry.Services))
	for i, service := range entry.Services {
		rows[i] = appServiceRow{
			Name:     service.Name,
			ImageRef: usecase.ReleaseServiceImageRef(service) + " (not deployed)",
		}
	}
	m.writeAppServiceRows(b, appName, rows, lookups)
}

func (m model) writeAppServiceRows(b *strings.Builder, appName string, rows []appServiceRow, lookups appsSectionLookups) {
	for _, row := range rows {
		imageRef := dimStyle.Render(row.ImageRef)
		suffix := ""
		if cursor, ok := lookups.cursorsByAppService[appName+"/"+row.Name]; ok {
			suffix = m.formatServicePendingSuffix(cursor, row)
		}
		b.WriteString(fmt.Sprintf("    %-*s %s%s\n", serviceNameColumnWidth, row.Name, imageRef, suffix))
	}
}

func (m model) auditView() string {
	if len(m.audit) == 0 {
		return headerStyle.Render("Audit log") + "\n  " + dimStyle.Render("no events") + "\n"
	}
	return headerStyle.Render("Audit log") + "\n" + m.panel.View() + "\n"
}

func (m *model) resizePanel() {
	m.panel.SetWidth(m.contentWidth())
	m.bodyPanel.SetWidth(m.contentWidth())
	m.compact = false
	m.scrollDashboard = false

	if m.view == viewAudit {
		m.fitPanelToTerminal(lineCount(m.auditContent()))
		return
	}
	if m.view != viewDashboard || m.busy || m.data == nil {
		m.panel.SetHeight(minPanelHeight)
		return
	}
	if m.hasDetailPanel() {
		m.fitPanelToTerminal(lineCount(m.detailContent()))
		if m.height > 0 && m.visibleViewLines() > m.height {
			m.compact = true
			m.fitPanelToTerminal(lineCount(m.detailContent()))
		}
	}
	if m.height > 0 && m.visibleViewLines() > m.height {
		m.scrollDashboard = true
		m.bodyPanel.SetHeight(m.bodyPanelHeight())
		m.bodyPanel.SetContent(m.dashboardScrollContent())
	}
}

func (m *model) fitPanelToTerminal(contentLines int) {
	m.panel.SetHeight(max(1, contentLines))
	for m.height > 0 && m.visibleViewLines() > m.height && m.panel.Height() > 1 {
		m.panel.SetHeight(m.panel.Height() - 1)
	}
}

func (m model) bodyPanelHeight() int {
	used := lineCount(m.header())
	used += lineCount(m.loadErrorBlock())
	used += lineCount(m.noticeBlock())
	used += 1
	used += lineCount(m.footer())
	return max(1, m.height-used)
}

func (m model) dashboardScrollContent() string {
	var b strings.Builder
	b.WriteString(m.configSection())
	b.WriteString(m.stackSummary())
	if m.detailContent() != "" {
		b.WriteString(m.detailPanelHeader())
		b.WriteString(m.detailContent())
		b.WriteString("\n")
	}
	b.WriteString(m.updatesSection())
	b.WriteString(m.appsSection())
	return strings.TrimRight(b.String(), "\n")
}

func (m model) visibleViewLines() int {
	return len(strings.Split(strings.TrimRight(m.View().Content, "\n"), "\n"))
}

func (m *model) updatePanelContent() {
	if m.view == viewAudit {
		m.panel.SetContent(m.auditContent())
		return
	}
	m.panel.SetContent(m.detailContent())
}

func (m model) loadErrorBlock() string {
	if m.loadErr == nil {
		return ""
	}
	return errStyle.Render(loadErrorPrefix+m.shortText(m.loadErr.Error(), m.contentWidth()-len(loadErrorPrefix))) + "\n\n"
}

func (m model) noticeBlock() string {
	if m.notice == "" {
		return ""
	}
	return noticeStyle.Render(m.shortText(m.notice, m.contentWidth())) + "\n\n"
}

func (m model) detailPanelHeader() string {
	return headerStyle.Render("Details") + "\n"
}

func (m model) auditContent() string {
	var b strings.Builder
	for _, event := range m.audit {
		b.WriteString(fmt.Sprintf("  %s  %-24s %-20s %s\n",
			event.CreatedAt.Format("01-02 15:04"), event.Type, event.ActorEmail, event.Message))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) detailContent() string {
	var details []string
	if m.loadErr != nil {
		details = append(details, "load error:\n"+m.loadErr.Error())
	}
	if m.notice != "" && textWasShortened(m.notice, m.contentWidth()) {
		details = append(details, m.notice)
	}
	if m.data != nil {
		details = append(details, m.overviewDetails()...)
	}
	return strings.Join(details, "\n\n")
}

func (m model) overviewDetails() []string {
	var details []string
	if m.data.cursor.LastError != "" {
		details = append(details, "sync error:\n"+m.data.cursor.LastError)
	}
	if m.data.stack.LastAttempt != nil {
		attempt := m.data.stack.LastAttempt
		if attempt.Error != "" {
			label := "last attempt error"
			if attempt.FailedApp != "" {
				label += " for " + attempt.FailedApp
			}
			details = append(details, label+":\n"+attempt.Error)
		}
		if attempt.RevertError != "" {
			details = append(details, "revert error:\n"+attempt.RevertError)
		}
	}
	for _, cursor := range m.data.cursors {
		if cursor.Status == imagewatch.StatusInvalid && cursor.LastError != "" {
			details = append(details, "image watch error for "+cursor.App+"/"+cursor.Service+":\n"+cursor.LastError)
		}
	}
	for _, entry := range m.data.appServices {
		if entry.ParseError != "" {
			details = append(details, "config error for "+entry.Name+":\n"+entry.ParseError)
		}
	}
	for _, baseline := range m.data.baselineReleases {
		if baseline.ManifestError != "" {
			details = append(details, "manifest error for "+baseline.Name+":\n"+baseline.ManifestError)
		}
	}
	return details
}

func (m model) hasDetailPanel() bool {
	return m.view == viewDashboard && m.detailContent() != ""
}

func (m model) scrollPanelActive() bool {
	if m.scrollDashboard {
		return true
	}
	return m.view == viewAudit || m.hasDetailPanel()
}

func (m model) contentWidth() int {
	if m.width <= 0 {
		return defaultWidth
	}
	return max(20, m.width-terminalSideMargin)
}

func (m model) panelContent() string {
	if m.view == viewAudit {
		return m.auditContent()
	}
	return m.detailContent()
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func (m model) shortText(text string, width int) string {
	return shortenText(text, width)
}

func shortenText(text string, width int) string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if !textWasShortened(trimmed, width) {
		return trimmed
	}
	if width <= ellipsisReserve {
		return "..."
	}
	runes := []rune(trimmed)
	return strings.TrimSpace(string(runes[:width-ellipsisReserve])) + "..."
}

func textWasShortened(text string, width int) bool {
	trimmed := strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	return len([]rune(trimmed)) > max(1, width)
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
		data.appServices, err = service.ListAppReleaseServices(ctx)
		if err != nil {
			return loadErrMsg{err: err}
		}
		data.baselineReleases, err = service.ResolveBaselineReleases(ctx, data.stack.LastHealthy.Releases)
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

func (m model) formatServicePendingSuffix(cursor imagewatch.Cursor, row appServiceRow) string {
	switch cursor.Status {
	case imagewatch.StatusUpdateAvailable:
		return noticeStyle.Render(" → " + cursor.CandidateTag + " (commit pending)")
	case imagewatch.StatusInvalid:
		fixed := serviceRowIndent + serviceNameColumnWidth + len(serviceImageSeparator) + len(row.ImageRef) + len(invalidStatusPrefix)
		return errStyle.Render(invalidStatusPrefix + m.shortText(cursor.LastError, m.contentWidth()-fixed))
	default:
		return ""
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
