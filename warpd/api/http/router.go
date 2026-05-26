// Package http wires Warpgate web routes to application use cases.
package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	webapi "github.com/pangobit/warpgate/warpd/api/web"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/usecase"
)

// NewRouter creates the warpd HTTP route tree.
func NewRouter(service *usecase.Service, identifier identity.Identifier, assets http.Handler) http.Handler {
	router := &router{
		service:    service,
		identifier: identifier,
		assets:     assets,
		renderer:   webapi.NewRenderer(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", router.health)
	mux.Handle("/assets/", assets)
	mux.Handle("/", router.requireAdmin(http.HandlerFunc(router.route)))
	return mux
}

type router struct {
	service    *usecase.Service
	identifier identity.Identifier
	assets     http.Handler
	renderer   *webapi.Renderer
}

func (r *router) route(w http.ResponseWriter, req *http.Request) {
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/":
		r.dashboard(w, req)
	case req.Method == http.MethodGet && req.URL.Path == "/apps":
		r.apps(w, req)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/apps/") && strings.HasSuffix(req.URL.Path, "/edit"):
		r.appEdit(w, req)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/apps/"):
		r.appDetail(w, req)
	case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/apps/") && strings.HasSuffix(req.URL.Path, "/commit"):
		r.commitRelease(w, req)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/releases/"):
		r.releaseDetail(w, req)
	case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/releases/") && strings.HasSuffix(req.URL.Path, "/deploy"):
		r.deployRelease(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/sync/config/check-now":
		r.syncConfig(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/sync/images/check-now":
		r.syncImages(w, req)
	case req.Method == http.MethodGet && req.URL.Path == "/settings":
		r.settings(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/settings/repository":
		r.attachRepository(w, req)
	default:
		http.NotFound(w, req)
	}
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
		next.ServeHTTP(w, req.WithContext(identity.WithUser(req.Context(), user)))
	})
}

func (r *router) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok\n")); err != nil {
		return
	}
}

func (r *router) dashboard(w http.ResponseWriter, req *http.Request) {
	dashboard, err := r.service.Dashboard(req.Context())
	page := webapi.DashboardPage{Title: "Warpgate", IdentityLabel: identityLabel(req), Dashboard: dashboard}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.Dashboard(page))
}

func (r *router) apps(w http.ResponseWriter, req *http.Request) {
	apps, err := r.service.Apps(req.Context())
	page := webapi.AppsPage{Title: "Apps", IdentityLabel: identityLabel(req), Apps: apps}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.Apps(page))
}

func (r *router) appDetail(w http.ResponseWriter, req *http.Request) {
	appName := strings.TrimPrefix(req.URL.Path, "/apps/")
	detail, err := r.service.AppDetail(req.Context(), appName)
	page := webapi.AppDetailPage{Title: appName, IdentityLabel: identityLabel(req), Detail: detail}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.AppDetail(page))
}

func (r *router) appEdit(w http.ResponseWriter, req *http.Request) {
	appName := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/apps/"), "/edit")
	detail, err := r.service.AppDetail(req.Context(), appName)
	serviceName := ""
	if len(detail.App.RawYAML) > 0 {
		serviceName = appName
	}
	page := webapi.AppEditPage{Title: "Edit " + appName, IdentityLabel: identityLabel(req), App: detail.App, Service: serviceName}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.AppEdit(page))
}

func (r *router) commitRelease(w http.ResponseWriter, req *http.Request) {
	user, _ := identity.UserFrom(req.Context())
	appName := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/apps/"), "/commit")
	if err := req.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	change := release.DeployDataChange{
		Service:     req.Form.Get("service"),
		ImageTag:    req.Form.Get("image_tag"),
		ImageDigest: req.Form.Get("image_digest"),
	}
	record, err := r.service.CommitRelease(req.Context(), user, appName, []release.DeployDataChange{change})
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

func (r *router) releaseDetail(w http.ResponseWriter, req *http.Request) {
	releaseID := strings.TrimPrefix(req.URL.Path, "/releases/")
	record, ok, err := r.service.Release(req.Context(), releaseID)
	page := webapi.ReleasePage{Title: "Release " + releaseID, IdentityLabel: identityLabel(req), Release: record}
	if err != nil {
		page.Error = err.Error()
	} else if !ok {
		page.Error = "release not found"
		w.WriteHeader(http.StatusNotFound)
	}
	r.render(w, webapi.ReleaseDetail(page))
}

func (r *router) deployRelease(w http.ResponseWriter, req *http.Request) {
	user, _ := identity.UserFrom(req.Context())
	releaseID := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/releases/"), "/deploy")
	if _, err := r.service.DeployRelease(req.Context(), user, releaseID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, req, "/releases/"+releaseID, http.StatusSeeOther)
}

func (r *router) syncConfig(w http.ResponseWriter, req *http.Request) {
	user, _ := identity.UserFrom(req.Context())
	if err := r.service.SyncConfig(req.Context(), user); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, req, "/", http.StatusSeeOther)
}

func (r *router) syncImages(w http.ResponseWriter, req *http.Request) {
	user, _ := identity.UserFrom(req.Context())
	if err := r.service.CheckImages(req.Context(), user); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, req, "/apps", http.StatusSeeOther)
}

func (r *router) settings(w http.ResponseWriter, req *http.Request) {
	settings, _, err := r.service.RepositorySettings(req.Context())
	page := webapi.SettingsPage{Title: "Settings", IdentityLabel: identityLabel(req), Repository: settings}
	if err != nil {
		page.Error = err.Error()
	}
	r.render(w, webapi.Settings(page))
}

func (r *router) attachRepository(w http.ResponseWriter, req *http.Request) {
	user, _ := identity.UserFrom(req.Context())
	if err := req.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	settings := configrepo.RepositorySettings{
		Owner:       req.Form.Get("owner"),
		Repo:        req.Form.Get("repo"),
		Branch:      req.Form.Get("branch"),
		TokenEnvVar: req.Form.Get("token_env_var"),
	}
	if err := r.service.AttachRepository(req.Context(), user, settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, req, "/settings", http.StatusSeeOther)
}

func (r *router) render(w http.ResponseWriter, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.renderer.Render(w, component); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
