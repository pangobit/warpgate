// Package turso provides Warpgate operational state stores.
package turso

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/audit"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/deployment"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/release"
)

// MemoryStore is an in-process store used by tests and local fallback.
type MemoryStore struct {
	mu sync.Mutex

	repoSettings    configrepo.RepositorySettings
	repoAttached    bool
	githubSession   identity.GitHubSession
	githubConnected bool
	pollerSettings  configrepo.PollerSettings
	configCursor    configrepo.SyncCursor
	apps            map[string]configrepo.AppSnapshot
	imageCursors    map[string]imagewatch.Cursor
	releases        map[string]release.Record
	deployments     map[string]deployment.Record
	auditEvents     []audit.Event
	deploymentOrder []string
	releaseOrder    []string
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		pollerSettings: configrepo.PollerSettings{
			ConfigEnabled:  true,
			ConfigInterval: 5 * time.Minute,
			ImagesEnabled:  true,
			ImagesInterval: 15 * time.Minute,
		},
		apps:         make(map[string]configrepo.AppSnapshot),
		imageCursors: make(map[string]imagewatch.Cursor),
		releases:     make(map[string]release.Record),
		deployments:  make(map[string]deployment.Record),
	}
}

// RepositorySettings returns the attached repository settings.
func (s *MemoryStore) RepositorySettings(_ context.Context) (configrepo.RepositorySettings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repoSettings, s.repoAttached, nil
}

// SaveRepositorySettings persists repository settings.
func (s *MemoryStore) SaveRepositorySettings(_ context.Context, settings configrepo.RepositorySettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repoSettings = settings
	s.repoAttached = true
	return nil
}

// GitHubSession returns the persisted GitHub authorization.
func (s *MemoryStore) GitHubSession(_ context.Context) (identity.GitHubSession, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.githubSession, s.githubConnected, nil
}

// SaveGitHubSession persists GitHub authorization.
func (s *MemoryStore) SaveGitHubSession(_ context.Context, session identity.GitHubSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubSession = session
	s.githubConnected = true
	return nil
}

// DeleteGitHubSession removes persisted GitHub authorization.
func (s *MemoryStore) DeleteGitHubSession(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubSession = identity.GitHubSession{}
	s.githubConnected = false
	return nil
}

// PollerSettings returns persisted poller settings.
func (s *MemoryStore) PollerSettings(_ context.Context) (configrepo.PollerSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pollerSettings, nil
}

// SavePollerSettings persists poller settings.
func (s *MemoryStore) SavePollerSettings(_ context.Context, settings configrepo.PollerSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pollerSettings = settings
	return nil
}

// ConfigCursor returns the config sync cursor.
func (s *MemoryStore) ConfigCursor(_ context.Context) (configrepo.SyncCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configCursor, nil
}

// SaveConfigCursor persists the config sync cursor.
func (s *MemoryStore) SaveConfigCursor(_ context.Context, cursor configrepo.SyncCursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configCursor = cursor
	return nil
}

// UpsertApp creates or updates an app snapshot.
func (s *MemoryStore) UpsertApp(_ context.Context, app configrepo.AppSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps[app.Name] = app
	return nil
}

// App returns one app snapshot.
func (s *MemoryStore) App(_ context.Context, name string) (configrepo.AppSnapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, ok := s.apps[name]
	return app, ok, nil
}

// ListApps returns all app snapshots sorted by name.
func (s *MemoryStore) ListApps(_ context.Context) ([]configrepo.AppSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	apps := make([]configrepo.AppSnapshot, 0, len(s.apps))
	for _, app := range s.apps {
		apps = append(apps, app)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps, nil
}

// SaveImageCursor creates or updates an image watch cursor.
func (s *MemoryStore) SaveImageCursor(_ context.Context, cursor imagewatch.Cursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.imageCursors[cursor.App+"/"+cursor.Service] = cursor
	return nil
}

// ListImageCursors returns all image watch cursors.
func (s *MemoryStore) ListImageCursors(_ context.Context) ([]imagewatch.Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursors := make([]imagewatch.Cursor, 0, len(s.imageCursors))
	for _, cursor := range s.imageCursors {
		cursors = append(cursors, cursor)
	}
	sort.Slice(cursors, func(i, j int) bool {
		if cursors[i].App == cursors[j].App {
			return cursors[i].Service < cursors[j].Service
		}
		return cursors[i].App < cursors[j].App
	})
	return cursors, nil
}

// CreateRelease persists a release record.
func (s *MemoryStore) CreateRelease(_ context.Context, record release.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.releases[record.ID]; !exists {
		s.releaseOrder = append(s.releaseOrder, record.ID)
	}
	s.releases[record.ID] = record
	return nil
}

// UpdateReleaseStatus changes a release status.
func (s *MemoryStore) UpdateReleaseStatus(_ context.Context, id string, status release.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.releases[id]
	if !ok {
		return fmt.Errorf("release %s not found", id)
	}
	record.Status = status
	s.releases[id] = record
	return nil
}

// Release returns one release record.
func (s *MemoryStore) Release(_ context.Context, id string) (release.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.releases[id]
	return record, ok, nil
}

// ListReleases returns release records for one app, newest first.
func (s *MemoryStore) ListReleases(_ context.Context, app string) ([]release.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []release.Record
	for i := len(s.releaseOrder) - 1; i >= 0; i-- {
		record := s.releases[s.releaseOrder[i]]
		if app == "" || record.App == app {
			records = append(records, record)
		}
	}
	return records, nil
}

// CreateDeployment persists a deployment attempt.
func (s *MemoryStore) CreateDeployment(_ context.Context, record deployment.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.deployments[record.ID]; !exists {
		s.deploymentOrder = append(s.deploymentOrder, record.ID)
	}
	s.deployments[record.ID] = record
	return nil
}

// FinishDeployment records deployment completion.
func (s *MemoryStore) FinishDeployment(_ context.Context, id string, status deployment.Status, errorMessage string, finishedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.deployments[id]
	if !ok {
		return fmt.Errorf("deployment %s not found", id)
	}
	record.Status = status
	record.ErrorMessage = errorMessage
	record.FinishedAt = &finishedAt
	s.deployments[id] = record
	return nil
}

// ListDeployments returns deployment records for one app, newest first.
func (s *MemoryStore) ListDeployments(_ context.Context, app string) ([]deployment.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []deployment.Record
	for i := len(s.deploymentOrder) - 1; i >= 0; i-- {
		record := s.deployments[s.deploymentOrder[i]]
		if app == "" || record.App == app {
			records = append(records, record)
		}
	}
	return records, nil
}

// AddAuditEvent persists an audit event.
func (s *MemoryStore) AddAuditEvent(_ context.Context, event audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditEvents = append(s.auditEvents, event)
	return nil
}

// ListAuditEvents returns recent audit events, newest first.
func (s *MemoryStore) ListAuditEvents(_ context.Context, limit int) ([]audit.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.auditEvents) {
		limit = len(s.auditEvents)
	}
	events := make([]audit.Event, 0, limit)
	for i := len(s.auditEvents) - 1; i >= 0 && len(events) < limit; i-- {
		events = append(events, s.auditEvents[i])
	}
	return events, nil
}
