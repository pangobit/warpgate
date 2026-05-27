// Package http wires Warpgate web routes to application use cases.
package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	webapi "github.com/pangobit/warpgate/warpd/api/web"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/usecase"
)

const githubClientIDCookieName = "warpgate_github_client_id"

// NewRouter creates the warpd HTTP route tree.
func NewRouter(service *usecase.Service, identifier identity.Identifier, assets http.Handler, options ...RouterOption) http.Handler {
	router := &router{
		service:    service,
		identifier: identifier,
		assets:     assets,
		renderer:   webapi.NewRenderer(),
	}
	for _, option := range options {
		option(router)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", router.health)
	mux.Handle("/assets/", assets)
	protected := router.requireAdmin
	mux.Handle("GET /{$}", protected(http.HandlerFunc(router.dashboard)))
	mux.Handle("GET /apps", protected(http.HandlerFunc(router.apps)))
	mux.Handle("GET /apps/{app}", protected(http.HandlerFunc(router.appDetail)))
	mux.Handle("GET /apps/{app}/edit", protected(http.HandlerFunc(router.appEdit)))
	mux.Handle("POST /apps/{app}/commit", protected(http.HandlerFunc(router.commitRelease)))
	mux.Handle("GET /releases/{release}", protected(http.HandlerFunc(router.releaseDetail)))
	mux.Handle("POST /releases/{release}/deploy", protected(http.HandlerFunc(router.deployRelease)))
	mux.Handle("POST /sync/config/check-now", protected(http.HandlerFunc(router.syncConfig)))
	mux.Handle("POST /sync/images/check-now", protected(http.HandlerFunc(router.syncImages)))
	mux.Handle("GET /status", protected(http.HandlerFunc(router.runtimeStatus)))
	mux.Handle("GET /logs", protected(http.HandlerFunc(router.logs)))
	mux.Handle("GET /settings", protected(http.HandlerFunc(router.settings)))
	mux.Handle("POST /settings/repository", protected(http.HandlerFunc(router.attachRepository)))
	mux.Handle("POST /auth/github/start", protected(http.HandlerFunc(router.startGitHubAuth)))
	mux.Handle("POST /auth/github/complete", protected(http.HandlerFunc(router.completeGitHubAuth)))
	mux.Handle("POST /auth/github/disconnect", protected(http.HandlerFunc(router.disconnectGitHub)))
	mux.Handle("/", protected(http.NotFoundHandler()))
	return mux
}

// RouterOption configures the Warpgate HTTP route tree.
type RouterOption func(*router)

// WithGitHubAuth enables local GitHub App authorization routes.
func WithGitHubAuth(auth GitHubAuthenticator) RouterOption {
	return func(router *router) {
		router.githubAuth = auth
	}
}

// GitHubAuthenticator manages a local GitHub authorization session.
type GitHubAuthenticator interface {
	CompleteDeviceFlow(ctx context.Context) error
	Disconnect()
	SetClientID(clientID string)
	StartDeviceFlow(ctx context.Context) error
	Status() identity.GitHubAuthStatus
}

type router struct {
	service    *usecase.Service
	identifier identity.Identifier
	assets     http.Handler
	renderer   *webapi.Renderer
	githubAuth GitHubAuthenticator
}

func (r *router) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, err := r.identifier.Identify(req.Context(), req.RemoteAddr)
		if err != nil {
			http.Error(w, "Unauthenticated", http.StatusUnauthorized)
			return
		}
		if err := identity.RequireAdmin(user); err != nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		r.applyGitHubClientID(req)
		next.ServeHTTP(w, req.WithContext(identity.WithUser(req.Context(), user)))
	})
}

func (r *router) currentUser(w http.ResponseWriter, req *http.Request) (identity.User, bool) {
	user, ok := identity.UserFrom(req.Context())
	if !ok {
		http.Error(w, "Unauthenticated", http.StatusUnauthorized)
		return identity.User{}, false
	}
	if err := identity.RequireAdmin(user); err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return identity.User{}, false
	}
	return user, true
}

func (r *router) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok\n")); err != nil {
		return
	}
}

func (r *router) dashboard(w http.ResponseWriter, req *http.Request) {
	dashboard, err := r.service.Dashboard(req.Context())
	page := webapi.DashboardPage{Title: "Warpgate", IdentityLabel: identityLabel(req), Dashboard: dashboardView(dashboard)}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.Dashboard(page))
}

func (r *router) apps(w http.ResponseWriter, req *http.Request) {
	apps, err := r.service.Apps(req.Context())
	page := webapi.AppsPage{Title: "Apps", IdentityLabel: identityLabel(req), Apps: appViews(apps)}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.Apps(page))
}

func (r *router) runtimeStatus(w http.ResponseWriter, req *http.Request) {
	user, ok := r.currentUser(w, req)
	if !ok {
		return
	}
	status, err := r.service.RuntimeStatus(req.Context(), user)
	page := webapi.StatusPage{Title: "Status", IdentityLabel: identityLabel(req), Status: runtimeStatusView(status)}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.Status(page))
}

func (r *router) logs(w http.ResponseWriter, req *http.Request) {
	user, ok := r.currentUser(w, req)
	if !ok {
		return
	}
	apps, appsErr := r.service.Apps(req.Context())
	nodes, nodesErr := r.service.ConfigNodes(req.Context(), user)
	input, requested, inputErr := logsInputFromRequest(req)
	page := webapi.LogsPage{
		Title:         "Logs",
		IdentityLabel: identityLabel(req),
		Request:       logsRequestView(input),
		Apps:          appViews(apps),
		Nodes:         configNodeViews(nodes),
	}
	if appsErr != nil {
		page.Error = appsErr.Error()
		r.render(w, webapi.Logs(page))
		return
	}
	if nodesErr != nil {
		page.Error = nodesErr.Error()
		r.render(w, webapi.Logs(page))
		return
	}
	if inputErr != nil {
		page.Error = inputErr.Error()
		r.renderStatus(w, http.StatusBadRequest, webapi.Logs(page))
		return
	}
	if requested {
		result, err := r.service.Logs(req.Context(), user, input)
		page.Result = logsResultView(result)
		page.HasResult = err == nil
		if err != nil {
			page.Error = err.Error()
			r.renderStatus(w, http.StatusBadRequest, webapi.Logs(page))
			return
		}
	}
	r.render(w, webapi.Logs(page))
}

func (r *router) appDetail(w http.ResponseWriter, req *http.Request) {
	appName := strings.TrimPrefix(req.URL.Path, "/apps/")
	detail, err := r.service.AppDetail(req.Context(), appName)
	page := webapi.AppDetailPage{Title: appName, IdentityLabel: identityLabel(req), Detail: appDetailView(detail)}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.AppDetail(page))
}

func (r *router) appEdit(w http.ResponseWriter, req *http.Request) {
	appName := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/apps/"), "/edit")
	detail, err := r.service.AppDetail(req.Context(), appName)
	page := webapi.AppEditPage{Title: "Edit " + appName, IdentityLabel: identityLabel(req), App: appView(detail.App), Services: appReleaseServiceViews(detail.Services)}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.AppEdit(page))
}

func (r *router) commitRelease(w http.ResponseWriter, req *http.Request) {
	user, ok := r.currentUser(w, req)
	if !ok {
		return
	}
	appName := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/apps/"), "/commit")
	if err := req.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	record, err := r.service.CommitRelease(req.Context(), user, appName, deployDataChangesFromForm(req))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, usecase.ErrConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	http.Redirect(w, req, "/releases/"+record.ID, http.StatusSeeOther)
}

func deployDataChangesFromForm(req *http.Request) []release.DeployDataChange {
	services := req.PostForm["service"]
	changes := make([]release.DeployDataChange, 0, len(services))
	for index, service := range services {
		changes = append(changes, release.DeployDataChange{
			Service:     strings.TrimSpace(service),
			ImageTag:    strings.TrimSpace(formValueAt(req.PostForm, "image_tag", index)),
			ImageDigest: strings.TrimSpace(formValueAt(req.PostForm, "image_digest", index)),
		})
	}
	return changes
}

func formValueAt(values map[string][]string, key string, index int) string {
	items := values[key]
	if index < 0 || index >= len(items) {
		return ""
	}
	return items[index]
}

func (r *router) releaseDetail(w http.ResponseWriter, req *http.Request) {
	releaseID := strings.TrimPrefix(req.URL.Path, "/releases/")
	record, ok, err := r.service.Release(req.Context(), releaseID)
	page := webapi.ReleasePage{Title: "Release " + releaseID, IdentityLabel: identityLabel(req), Release: releaseView(record)}
	if err != nil {
		page.Error = err.Error()
	} else if !ok {
		page.Error = "release not found"
		w.WriteHeader(http.StatusNotFound)
	}
	r.render(w, webapi.ReleaseDetail(page))
}

func (r *router) deployRelease(w http.ResponseWriter, req *http.Request) {
	user, ok := r.currentUser(w, req)
	if !ok {
		return
	}
	releaseID := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/releases/"), "/deploy")
	if _, err := r.service.DeployRelease(req.Context(), user, releaseID); err != nil {
		record, _, releaseErr := r.service.Release(req.Context(), releaseID)
		page := webapi.ReleasePage{Title: "Release " + releaseID, IdentityLabel: identityLabel(req), Release: releaseView(record), Error: err.Error()}
		if releaseErr != nil {
			page.Error = errors.Join(err, releaseErr).Error()
		}
		r.renderStatus(w, http.StatusBadRequest, webapi.ReleaseDetail(page))
		return
	}
	http.Redirect(w, req, "/releases/"+releaseID, http.StatusSeeOther)
}

func (r *router) syncConfig(w http.ResponseWriter, req *http.Request) {
	user, ok := r.currentUser(w, req)
	if !ok {
		return
	}
	if err := r.service.SyncConfig(req.Context(), user); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, req, "/", http.StatusSeeOther)
}

func (r *router) syncImages(w http.ResponseWriter, req *http.Request) {
	user, ok := r.currentUser(w, req)
	if !ok {
		return
	}
	if err := r.service.CheckImages(req.Context(), user); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, req, "/apps", http.StatusSeeOther)
}

func (r *router) settings(w http.ResponseWriter, req *http.Request) {
	settings, _, err := r.service.RepositorySettings(req.Context())
	page := r.settingsPage(req, settings)
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.Settings(page))
}

func (r *router) attachRepository(w http.ResponseWriter, req *http.Request) {
	user, ok := r.currentUser(w, req)
	if !ok {
		return
	}
	if err := req.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	settings := configrepo.RepositorySettings{
		Owner:  req.Form.Get("owner"),
		Repo:   req.Form.Get("repo"),
		Branch: req.Form.Get("branch"),
		Path:   req.Form.Get("path"),
	}
	if err := r.service.AttachRepository(req.Context(), user, settings); err != nil {
		page := r.settingsPage(req, settings)
		page.Error = err.Error()
		r.renderStatus(w, http.StatusBadRequest, webapi.Settings(page))
		return
	}
	http.Redirect(w, req, "/settings", http.StatusSeeOther)
}

func (r *router) startGitHubAuth(w http.ResponseWriter, req *http.Request) {
	if r.githubAuth == nil {
		http.Error(w, "GitHub App authorization is not configured", http.StatusBadRequest)
		return
	}
	if clientID, ok, err := githubClientIDFromForm(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if ok {
		r.githubAuth.SetClientID(clientID)
		setGitHubClientIDCookie(w, req, clientID)
	}
	if err := r.githubAuth.StartDeviceFlow(req.Context()); err != nil {
		http.Redirect(w, req, "/settings", http.StatusSeeOther)
		return
	}
	http.Redirect(w, req, "/settings", http.StatusSeeOther)
}

func (r *router) completeGitHubAuth(w http.ResponseWriter, req *http.Request) {
	if r.githubAuth == nil {
		http.Error(w, "GitHub App authorization is not configured", http.StatusBadRequest)
		return
	}
	if err := r.githubAuth.CompleteDeviceFlow(req.Context()); err != nil {
		http.Redirect(w, req, "/settings", http.StatusSeeOther)
		return
	}
	http.Redirect(w, req, "/settings", http.StatusSeeOther)
}

func (r *router) disconnectGitHub(w http.ResponseWriter, req *http.Request) {
	if r.githubAuth != nil {
		r.githubAuth.Disconnect()
	}
	http.Redirect(w, req, "/settings", http.StatusSeeOther)
}

func (r *router) render(w http.ResponseWriter, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.renderer.Render(w, component); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *router) renderStatus(w http.ResponseWriter, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := r.renderer.Render(w, component); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *router) githubAuthStatus() identity.GitHubAuthStatus {
	if r.githubAuth == nil {
		return identity.GitHubAuthStatus{}
	}
	return r.githubAuth.Status()
}

func (r *router) applyGitHubClientID(req *http.Request) {
	if r.githubAuth == nil {
		return
	}
	clientID := githubClientIDFromCookie(req)
	if clientID == "" {
		return
	}
	r.githubAuth.SetClientID(clientID)
}

func githubClientIDFromForm(req *http.Request) (string, bool, error) {
	if err := req.ParseForm(); err != nil {
		return "", false, err
	}
	clientID := strings.TrimSpace(req.Form.Get("github_client_id"))
	if clientID == "" {
		return "", false, nil
	}
	if err := validateGitHubClientID(clientID); err != nil {
		return "", false, err
	}
	return clientID, true, nil
}

func githubClientIDFromCookie(req *http.Request) string {
	cookie, err := req.Cookie(githubClientIDCookieName)
	if err != nil {
		return ""
	}
	clientID := strings.TrimSpace(cookie.Value)
	if validateGitHubClientID(clientID) != nil {
		return ""
	}
	return clientID
}

func setGitHubClientIDCookie(w http.ResponseWriter, req *http.Request, clientID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     githubClientIDCookieName,
		Value:    clientID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   req.TLS != nil,
	})
}

func validateGitHubClientID(clientID string) error {
	if len(clientID) > 128 {
		return errors.New("GitHub App client ID is too long")
	}
	for _, char := range clientID {
		if isGitHubClientIDChar(char) {
			continue
		}
		return errors.New("GitHub App client ID contains invalid characters")
	}
	return nil
}

func isGitHubClientIDChar(char rune) bool {
	if char >= 'a' && char <= 'z' {
		return true
	}
	if char >= 'A' && char <= 'Z' {
		return true
	}
	if char >= '0' && char <= '9' {
		return true
	}
	return char == '.' || char == '_' || char == '-'
}

func (r *router) settingsPage(req *http.Request, settings configrepo.RepositorySettings) webapi.SettingsPage {
	return webapi.SettingsPage{
		Title:         "Settings",
		IdentityLabel: identityLabel(req),
		Repository:    repositoryView(settings),
		GitHubAuth:    githubAuthView(r.githubAuthStatus()),
	}
}

func identityLabel(req *http.Request) string {
	user, ok := identity.UserFrom(req.Context())
	if !ok {
		return "unknown"
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.Email != "" {
		return user.Email
	}
	return "unknown"
}

func logsInputFromRequest(req *http.Request) (usecase.LogsInput, bool, error) {
	query := req.URL.Query()
	tail := 100
	if value := strings.TrimSpace(query.Get("tail")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return usecase.LogsInput{}, false, errors.New("tail must be a number")
		}
		tail = parsed
	}
	input := usecase.LogsInput{
		NodeID: query.Get("node"),
		App:    query.Get("app"),
		Tail:   tail,
		Grep:   query.Get("grep"),
	}
	return input, strings.TrimSpace(input.NodeID) != "", nil
}
