package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/deploy"
)

// DashboardFetchFunc fetches cluster status. Injected by the caller to avoid
// the dashboard model needing a direct Deployer reference.
type DashboardFetchFunc func() (*deploy.ClusterStatusResult, error)

// DashboardConfig holds configuration for the dashboard TUI.
type DashboardConfig struct {
	// Project is the cluster project name shown in the title.
	Project string
	// Nodes is the list of cluster nodes.
	Nodes []config.NodeConfig
	// Apps is the list of all discovered apps.
	Apps []*config.AppConfig
	// Fetch retrieves the current cluster status.
	Fetch DashboardFetchFunc
	// RefreshInterval is the auto-refresh interval.
	RefreshInterval time.Duration
}

type dataMsg struct {
	result *deploy.ClusterStatusResult
}

type errorMsg struct {
	err error
}

type tickMsg time.Time

// DashboardModel is the bubbletea model for the cluster dashboard.
type DashboardModel struct {
	config      DashboardConfig
	result      *deploy.ClusterStatusResult
	spinner     spinner.Model
	nodesTable  table.Model
	appsTable   table.Model
	loading     bool
	lastErr     error
	lastRefresh time.Time
}

// NewDashboard creates a new dashboard model.
func NewDashboard(cfg DashboardConfig) DashboardModel {
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = 30 * time.Second
	}

	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))

	nodesTable := table.New(
		table.WithColumns(nodeColumns),
		table.WithWidth(tableWidth(nodeColumns)),
		table.WithStyles(dashTableStyles),
	)

	appsTable := table.New(
		table.WithColumns(appColumns),
		table.WithWidth(tableWidth(appColumns)),
		table.WithStyles(dashTableStyles),
	)

	return DashboardModel{
		config:     cfg,
		spinner:    s,
		nodesTable: nodesTable,
		appsTable:  appsTable,
		loading:    true,
	}
}

// Init starts the spinner, triggers the first data fetch, and starts the refresh tick.
func (m DashboardModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetch(), m.tick())
}

// Update handles messages.
func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, tea.Batch(m.spinner.Tick, m.fetch())
			}
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case dataMsg:
		m.result = msg.result
		m.loading = false
		m.lastErr = nil
		m.lastRefresh = time.Now()
		m.rebuildTables()
		return m, nil

	case errorMsg:
		m.loading = false
		m.lastErr = msg.err
		m.lastRefresh = time.Now()
		return m, nil

	case tickMsg:
		if !m.loading {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.fetch(), m.tick())
		}
		return m, m.tick()
	}

	return m, nil
}

// View renders the dashboard.
func (m DashboardModel) View() tea.View {
	var b strings.Builder

	title := dashTitleStyle.Render("Warpgate Dashboard") + dashDimStyle.Render(" — "+m.config.Project)
	refreshInfo := m.refreshStatus()
	b.WriteString(title + "  " + dashDimStyle.Render(refreshInfo))
	b.WriteString("\n\n")

	if m.lastErr != nil {
		b.WriteString(dashErrorStyle.Render("Error: "+m.lastErr.Error()) + "\n\n")
	}

	if m.result != nil {
		b.WriteString(dashHeaderStyle.Render("Nodes"))
		b.WriteString("\n")
		b.WriteString(m.nodesTable.View())
		b.WriteString("\n\n")

		b.WriteString(dashHeaderStyle.Render("Apps"))
		b.WriteString("\n")
		b.WriteString(m.appsTable.View())
	} else if m.loading {
		b.WriteString("  " + m.spinner.View() + " Fetching status...")
	}

	b.WriteString("\n\n")
	b.WriteString(dashDimStyle.Render("  [r] refresh  [q] quit"))
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m *DashboardModel) rebuildTables() {
	m.rebuildNodesTable()
	m.rebuildAppsTable()
}

func (m *DashboardModel) rebuildNodesTable() {
	var rows []table.Row
	for _, node := range m.config.Nodes {
		reachable := m.nodeReachable(node.ID)
		status := dashNodeOnlineStyle.Render(statusDot + " reachable")
		if !reachable {
			status = dashNodeOfflineStyle.Render(statusDot + " unreachable")
		}

		host := node.Host
		if len(host) > 14 {
			host = host[:14] + ".."
		}

		rows = append(rows, table.Row{node.ID, host, node.PrivateIP, status})
	}

	m.nodesTable.SetRows(rows)
	m.nodesTable.SetHeight(len(rows) + 1)
}

func (m *DashboardModel) rebuildAppsTable() {
	var rows []table.Row
	if m.result == nil {
		return
	}

	for _, app := range m.result.Apps {
		version := app.Version
		if version == "" {
			version = "-"
		}
		slot := app.Slot
		if slot == "" {
			slot = "-"
		}
		state := app.State
		if app.Error != "" {
			state = "error"
		}
		if state == "" {
			state = "-"
		}

		rows = append(rows, table.Row{app.App, version, slot, styleState(state), app.NodeID})

		for _, sc := range app.Sidecars {
			rows = append(rows, table.Row{
				dashDimStyle.Render("└ " + sc.Service), "", "", styleState(sc.State), "",
			})
		}
	}

	m.appsTable.SetRows(rows)
	m.appsTable.SetHeight(len(rows) + 1)
}

func (m DashboardModel) refreshStatus() string {
	if m.loading {
		return m.spinner.View() + " refreshing..."
	}
	if m.lastRefresh.IsZero() {
		return ""
	}
	ago := time.Since(m.lastRefresh).Truncate(time.Second)
	return fmt.Sprintf("Last refresh: %s ago", ago)
}

func (m DashboardModel) nodeReachable(nodeID string) bool {
	if m.result == nil {
		return false
	}
	reachable, ok := m.result.NodeReachable[nodeID]
	return ok && reachable
}

func styleState(state string) string {
	switch state {
	case "healthy":
		return dashHealthyStyle.Render(statusDot + " healthy")
	case "running":
		return dashRunningStyle.Render(statusDot + " running")
	case "unhealthy":
		return dashUnhealthyStyle.Render(statusDot + " unhealthy")
	case "not deployed":
		return dashDimStyle.Render("not deployed")
	case "error":
		return dashUnhealthyStyle.Render(statusDot + " error")
	default:
		return dashDimStyle.Render(state)
	}
}

func (m DashboardModel) fetch() tea.Cmd {
	fn := m.config.Fetch
	return func() tea.Msg {
		result, err := fn()
		if err != nil {
			return errorMsg{err: err}
		}
		return dataMsg{result: result}
	}
}

func (m DashboardModel) tick() tea.Cmd {
	d := m.config.RefreshInterval
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// RunDashboard creates a bubbletea program and runs the dashboard TUI to completion.
func RunDashboard(cfg DashboardConfig) error {
	m := NewDashboard(cfg)
	p := tea.NewProgram(m)
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("dashboard error: %w", err)
	}
	return nil
}

// Column definitions for dashboard tables.
var (
	nodeColumns = []table.Column{
		{Title: "ID", Width: 16},
		{Title: "HOST", Width: 16},
		{Title: "TAILSCALE", Width: 18},
		{Title: "STATUS", Width: 16},
	}

	appColumns = []table.Column{
		{Title: "NAME", Width: 22},
		{Title: "VERSION", Width: 14},
		{Title: "SLOT", Width: 8},
		{Title: "STATUS", Width: 16},
		{Title: "NODE", Width: 16},
	}
)

// tableWidth returns the total width for a set of columns, including cell padding.
func tableWidth(cols []table.Column) int {
	w := 0
	for _, c := range cols {
		w += c.Width + 2 // +2 for Padding(0, 1) on each side
	}
	return w
}

// Dashboard styles.
var (
	statusDot = "●"

	dashTitleStyle     = lipgloss.NewStyle().Bold(true)
	dashHeaderStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	dashDimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#606060"))
	dashErrorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4672"))

	dashHealthyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	dashRunningStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
	dashUnhealthyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4672"))
	dashNodeOnlineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	dashNodeOfflineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4672"))

	dashTableStyles = table.Styles{
		Header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#888888")).Padding(0, 1),
		Cell:     lipgloss.NewStyle().Padding(0, 1),
		Selected: lipgloss.NewStyle(),
	}
)
