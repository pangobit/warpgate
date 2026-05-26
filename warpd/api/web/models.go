package web

import (
	"context"
	"fmt"
	"io"

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
	// Service is the selected release service.
	Service string
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
