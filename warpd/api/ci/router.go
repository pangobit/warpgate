// Package ci exposes the lean Warpgate daemon HTTP API used by CI integrations.
package ci

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/stackstate"
	"github.com/pangobit/warpgate/warpd/usecase"
)

// NewRouter creates the daemon CI route tree.
// The refresh callback schedules an immediate repo and image poll.
func NewRouter(service *usecase.Service, identifier identity.Identifier, refresh func()) http.Handler {
	router := &router{service: service, identifier: identifier, refresh: refresh}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", router.health)
	mux.Handle("POST /refresh", router.requireAdmin(http.HandlerFunc(router.scheduleRefresh)))
	mux.Handle("GET /status", router.requireAdmin(http.HandlerFunc(router.status)))
	return mux
}

type router struct {
	service    *usecase.Service
	identifier identity.Identifier
	refresh    func()
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

func (r *router) scheduleRefresh(w http.ResponseWriter, _ *http.Request) {
	r.refresh()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "refresh scheduled"})
}

// StatusResponse is the daemon status payload returned to CI callers.
type StatusResponse struct {
	// Repository is the attached repository as owner/repo.
	Repository string `json:"repository"`
	// Branch is the watched branch.
	Branch string `json:"branch"`
	// ConfigCommit is the last observed desired-state commit.
	ConfigCommit string `json:"config_commit"`
	// ConfigCheckedAt is when the repository was last polled.
	ConfigCheckedAt time.Time `json:"config_checked_at"`
	// PendingUpdates lists services with a newer matching image version.
	PendingUpdates []PendingUpdate `json:"pending_updates"`
	// Stack is the whole-stack deploy state.
	Stack StackStatus `json:"stack"`
}

// PendingUpdate describes one service with an undeployed image bump.
type PendingUpdate struct {
	// App is the app name.
	App string `json:"app"`
	// Service is the release service name.
	Service string `json:"service"`
	// PinnedTag is the currently pinned tag.
	PinnedTag string `json:"pinned_tag"`
	// CandidateTag is the newer matching tag.
	CandidateTag string `json:"candidate_tag"`
}

// StackStatus summarizes whole-stack deploy state.
type StackStatus struct {
	// LastHealthy maps app names to the release IDs of the healthy baseline.
	LastHealthy map[string]string `json:"last_healthy"`
	// LastHealthyAt is when the baseline advanced.
	LastHealthyAt *time.Time `json:"last_healthy_at,omitempty"`
	// LastAttempt is the most recent stack deploy attempt.
	LastAttempt *stackstate.Attempt `json:"last_attempt,omitempty"`
}

func (r *router) status(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	settings, _, err := r.service.RepositorySettings(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dashboard, err := r.service.Dashboard(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cursors, err := r.service.ImageCursors(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	state, err := r.service.StackState(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := StatusResponse{
		Repository:      settings.Owner + "/" + settings.Repo,
		Branch:          settings.Branch,
		ConfigCommit:    dashboard.ConfigCursor.LastObservedCommit,
		ConfigCheckedAt: dashboard.ConfigCursor.LastCheckedAt,
		PendingUpdates:  pendingUpdates(cursors),
		Stack: StackStatus{
			LastHealthy: state.LastHealthy.Releases,
			LastAttempt: state.LastAttempt,
		},
	}
	if !state.LastHealthy.UpdatedAt.IsZero() {
		updatedAt := state.LastHealthy.UpdatedAt
		response.Stack.LastHealthyAt = &updatedAt
	}
	writeJSON(w, http.StatusOK, response)
}

func pendingUpdates(cursors []imagewatch.Cursor) []PendingUpdate {
	updates := make([]PendingUpdate, 0)
	for _, cursor := range cursors {
		if cursor.Status != imagewatch.StatusUpdateAvailable {
			continue
		}
		updates = append(updates, PendingUpdate{
			App:          cursor.App,
			Service:      cursor.Service,
			PinnedTag:    cursor.Tag,
			CandidateTag: cursor.CandidateTag,
		})
	}
	return updates
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return
	}
}
