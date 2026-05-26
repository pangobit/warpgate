package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/a-h/templ"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/usecase"
)

// DashboardPage is the dashboard template data.
type DashboardPage struct {
	// Title is the document title.
	Title string
	// IdentityLabel is the navigation identity label.
	IdentityLabel string
	// Error is a page-level error message.
	Error string
	// Dashboard is the dashboard view model.
	Dashboard usecase.Dashboard
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
	Apps []configrepo.AppSnapshot
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
	Status usecase.RuntimeStatus
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
	Request usecase.LogsInput
	// Result is the fetched logs result.
	Result usecase.LogsResult
	// Apps are observed app snapshots used as filter choices.
	Apps []configrepo.AppSnapshot
	// Nodes are cluster nodes used as target choices.
	Nodes []usecase.ConfigNode
	// HasResult reports whether the page should display a result panel.
	HasResult bool
}

type logLine struct {
	source string
	text   string
	json   bool
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
	Detail usecase.AppDetail
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
	App configrepo.AppSnapshot
	// Services are the editable release services.
	Services []usecase.AppReleaseService
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
	Release release.Record
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
	Repository configrepo.RepositorySettings
	// GitHubAuth is the local GitHub authorization state.
	GitHubAuth identity.GitHubAuthStatus
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

func githubAuthLabel(status identity.GitHubAuthStatus) string {
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

func githubAuthStatusStyle(status identity.GitHubAuthStatus) templ.CSSClass {
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
