package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/a-h/templ"
)

// RepositoryView identifies an attached GitHub infrastructure repository.
type RepositoryView struct {
	// Owner is the GitHub repository owner.
	Owner string
	// Repo is the GitHub repository name.
	Repo string
	// Branch is the branch Warpgate reads from and writes to.
	Branch string
	// Path is the optional repository subdirectory that contains cluster.yml and apps/.
	Path string
}

// SyncCursorView is the latest repository sync state.
type SyncCursorView struct {
	// LastObservedCommit is the latest branch head seen by Warpgate.
	LastObservedCommit string
	// LastCheckedAt is when the repository was last checked.
	LastCheckedAt time.Time
	// LastError is the last sync error, if any.
	LastError string
}

// DashboardView is the dashboard summary shown in the UI.
type DashboardView struct {
	// RepositoryAttached reports whether a repo is configured.
	RepositoryAttached bool
	// Repository is the attached repo settings.
	Repository RepositoryView
	// ConfigCursor is the latest config sync state.
	ConfigCursor SyncCursorView
	// AppCount is the number of observed apps.
	AppCount int
	// ImageUpdates is the count of watched images with digest changes.
	ImageUpdates int
}

// DashboardPage is the dashboard template data.
type DashboardPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Dashboard is the dashboard view model.
	Dashboard DashboardView
}

// AppView is an observed app config shown in the UI.
type AppView struct {
	// Name is the app name.
	Name string
	// Path is the repository path to the app config.
	Path string
	// ConfigCommit is the commit SHA that produced RawYAML.
	ConfigCommit string
	// RawYAML is the app.yml content.
	RawYAML string
}

// AppsPage is the apps list template data.
type AppsPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Apps are observed app snapshots.
	Apps []AppView
}

// RuntimeStatusView describes live cluster state for rendering.
type RuntimeStatusView struct {
	// Nodes are live node reachability records.
	Nodes []RuntimeNodeView
	// Apps are live app status records by node.
	Apps []RuntimeAppStatusView
}

// RuntimeNodeView describes one cluster node in the live status view.
type RuntimeNodeView struct {
	// ID is the node identifier.
	ID string
	// Host is the node SSH host.
	Host string
	// PrivateIP is the node private network address.
	PrivateIP string
	// Reachable reports whether Warpgate reached the node.
	Reachable bool
}

// RuntimeAppStatusView describes one app on one node.
type RuntimeAppStatusView struct {
	// App is the application name.
	App string
	// NodeID is the node identifier.
	NodeID string
	// Version is the currently deployed version.
	Version string
	// Slot is the active deployment slot.
	Slot string
	// State is the live app state.
	State string
	// Services are live service status rows.
	Services []RuntimeContainerStatusView
	// Error is set when live status could not be read.
	Error string
	// ShadowVersion is the current shadow version.
	ShadowVersion string
	// ShadowState is the current shadow state.
	ShadowState string
}

// RuntimeContainerStatusView describes a live compose service container.
type RuntimeContainerStatusView struct {
	// Service is the compose service name.
	Service string
	// Name is the container name.
	Name string
	// State is the live container state.
	State string
}

// StatusPage is the runtime status template data.
type StatusPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Status is the live runtime status view model.
	Status RuntimeStatusView
}

// LogsRequestView describes the current log query.
type LogsRequestView struct {
	// NodeID is the target node identifier.
	NodeID string
	// App filters logs to matching containers.
	App string
	// Tail is the number of recent lines.
	Tail int
	// Grep filters log lines server-side.
	Grep string
}

// LogsResultView describes fetched live logs.
type LogsResultView struct {
	// Output is raw prefixed log output.
	Output string
	// Message describes an empty result.
	Message string
}

// ConfigNodeView is a selectable cluster node.
type ConfigNodeView struct {
	// ID is the node identifier.
	ID string
	// Host is the node SSH host.
	Host string
	// PrivateIP is the node private network address.
	PrivateIP string
}

// LogsPage is the live logs template data.
type LogsPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Request is the current logs query.
	Request LogsRequestView
	// Result is the fetched logs result.
	Result LogsResultView
	// Apps are observed app snapshots used as filter choices.
	Apps []AppView
	// Nodes are cluster nodes used as target choices.
	Nodes []ConfigNodeView
	// HasResult reports whether the page should display a result panel.
	HasResult bool
}

type logLine struct {
	source string
	text   string
	json   bool
}

// AppDetailView is the app detail view model.
type AppDetailView struct {
	// App is the desired-state snapshot.
	App AppView
	// Services are the release services declared by app.yml.
	Services []AppReleaseServiceView
	// Releases are the app release records.
	Releases []ReleaseView
	// Deployments are the app deployment records.
	Deployments []DeploymentView
}

// AppReleaseServiceView describes an editable app release service.
type AppReleaseServiceView struct {
	// Name is the release service name.
	Name string
	// Image is the service image repository.
	Image string
	// ImageTag is the configured image tag.
	ImageTag string
	// ImageDigest is the configured image digest.
	ImageDigest string
}

// AppDetailPage is the app detail template data.
type AppDetailPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Detail is the app detail view model.
	Detail AppDetailView
}

// AppEditPage is the app edit template data.
type AppEditPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// App is the app snapshot being edited.
	App AppView
	// Services are the editable release services.
	Services []AppReleaseServiceView
}

// ReleaseView is a release record shown in the UI.
type ReleaseView struct {
	// ID is the release identifier.
	ID string
	// App is the app name.
	App string
	// ConfigCommit is the GitHub commit SHA that produced the release.
	ConfigCommit string
	// ManifestJSON is the immutable deploy manifest as JSON.
	ManifestJSON string
	// RawYAML is the committed app.yml content.
	RawYAML string
	// Status is the release lifecycle state.
	Status string
	// ActorEmail is the operator who created the release.
	ActorEmail string
	// CreatedAt is when the release was created.
	CreatedAt time.Time
}

// DeploymentView is a deployment attempt shown in the UI.
type DeploymentView struct {
	// ID is the deployment identifier.
	ID string
	// ReleaseID is the release being deployed.
	ReleaseID string
	// App is the app being deployed.
	App string
	// Targets are the requested target nodes.
	Targets []string
	// ActorEmail is the operator who started the deployment.
	ActorEmail string
	// Status is the deployment lifecycle state.
	Status string
	// StartedAt is when execution began.
	StartedAt time.Time
	// FinishedAt is when execution finished.
	FinishedAt *time.Time
	// ErrorMessage summarizes a failed deployment.
	ErrorMessage string
}

// ReleasePage is the release detail template data.
type ReleasePage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Release is the release record.
	Release ReleaseView
}

// GitHubAuthView describes local GitHub authorization state for rendering.
type GitHubAuthView struct {
	// Configured reports whether a GitHub App client ID is available.
	Configured bool
	// ClientID is the configured GitHub App client ID.
	ClientID string
	// Authenticated reports whether Warpgate has a usable GitHub token.
	Authenticated bool
	// Login is the authenticated GitHub login.
	Login string
	// DisplayName is the authenticated GitHub display name.
	DisplayName string
	// UserCode is the pending device-flow code.
	UserCode string
	// VerificationURI is the GitHub device-flow authorization URL.
	VerificationURI string
	// Error is the latest authorization error.
	Error string
}

// SettingsPage is the settings template data.
type SettingsPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Repository is the attached repository.
	Repository RepositoryView
	// GitHubAuth is the local GitHub authorization state.
	GitHubAuth GitHubAuthView
}

// Renderer renders Warpgate web components.
type Renderer struct{}

// NewRenderer creates a template renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// Render writes a templ component.
func (r *Renderer) Render(w io.Writer, component templ.Component) error {
	return component.Render(templ.WithChildren(templ.InitializeContext(context.Background()), templ.NopComponent), w)
}

func navigationIdentityLabel(label string) string {
	if label == "" {
		return "unknown"
	}
	return label
}

func identityAuthStyle(label string) templ.CSSClass {
	if navigationIdentityLabel(label) == "unknown" {
		return statusWarning()
	}
	return statusSuccess()
}

func githubAuthLabel(status GitHubAuthView) string {
	if status.Authenticated {
		if status.DisplayName != "" {
			return status.DisplayName
		}
		if status.Login != "" {
			return status.Login
		}
		return "connected"
	}
	return "unknown"
}

func githubAuthStatusStyle(status GitHubAuthView) templ.CSSClass {
	if status.Authenticated {
		return statusSuccess()
	}
	return statusWarning()
}

func tableRowStyle(index int) templ.CSSClass {
	if index%2 == 1 {
		return tableRowAlt()
	}
	return tableRow()
}

func statusClass(status string) string {
	switch status {
	case "deployed", "succeeded", "ready", "healthy":
		return statusSuccess().ClassName()
	case "failed", "invalid", "blocked", "unhealthy":
		return statusDanger().ClassName()
	default:
		return statusWarning().ClassName()
	}
}

func statusStyle(status any) templ.CSSClass {
	switch fmt.Sprint(status) {
	case "deployed", "succeeded", "ready", "healthy":
		return statusSuccess()
	case "failed", "invalid", "blocked", "unhealthy":
		return statusDanger()
	default:
		return statusWarning()
	}
}

func reachableLabel(reachable bool) string {
	if reachable {
		return "reachable"
	}
	return "unreachable"
}

func reachableStyle(reachable bool) templ.CSSClass {
	if reachable {
		return statusSuccess()
	}
	return statusDanger()
}

func logsText(result LogsPage) string {
	if result.Result.Message != "" {
		return result.Result.Message
	}
	return result.Result.Output
}

func parsedLogLines(result LogsPage) []logLine {
	raw := strings.TrimRight(result.Result.Output, "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	parsed := make([]logLine, 0, len(lines))
	for _, line := range lines {
		source, text := parseLogLine(line)
		formatted, ok := prettyJSON(text)
		parsed = append(parsed, logLine{source: source, text: formatted, json: ok})
	}
	return parsed
}

func parseLogLine(line string) (string, string) {
	if !strings.HasPrefix(line, "[") {
		return "output", line
	}
	source, text, ok := strings.Cut(line[1:], "] ")
	if !ok || source == "" {
		return "output", line
	}
	return source, text
}

func prettyJSON(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return value, false
	}
	data, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return value, false
	}
	return string(data), true
}
