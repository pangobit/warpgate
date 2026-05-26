package turso

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/audit"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/deployment"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/release"
	_ "turso.tech/database/tursogo"
)

// Store persists Warpgate state in embedded Turso.
type Store struct {
	db *sql.DB
}

// Open opens an embedded Turso database and applies the schema.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("turso", path)
	if err != nil {
		return nil, fmt.Errorf("open turso database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, errorsJoin(err, closeErr)
		}
		return nil, err
	}
	return store, nil
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create database directory %s: %w", dir, err)
	}
	return nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// RepositorySettings returns the attached repository settings.
func (s *Store) RepositorySettings(ctx context.Context) (configrepo.RepositorySettings, bool, error) {
	var settings configrepo.RepositorySettings
	ok, err := s.getJSON(ctx, "repo_settings", &settings)
	return settings, ok, err
}

// SaveRepositorySettings persists repository settings.
func (s *Store) SaveRepositorySettings(ctx context.Context, settings configrepo.RepositorySettings) error {
	return s.putJSON(ctx, "repo_settings", settings)
}

// PollerSettings returns persisted poller settings.
func (s *Store) PollerSettings(ctx context.Context) (configrepo.PollerSettings, error) {
	var settings configrepo.PollerSettings
	ok, err := s.getJSON(ctx, "poller_settings", &settings)
	if err != nil {
		return configrepo.PollerSettings{}, err
	}
	if !ok {
		return configrepo.PollerSettings{
			ConfigEnabled:  true,
			ConfigInterval: 5 * time.Minute,
			ImagesEnabled:  true,
			ImagesInterval: 15 * time.Minute,
		}, nil
	}
	return settings, nil
}

// SavePollerSettings persists poller settings.
func (s *Store) SavePollerSettings(ctx context.Context, settings configrepo.PollerSettings) error {
	return s.putJSON(ctx, "poller_settings", settings)
}

// ConfigCursor returns the config sync cursor.
func (s *Store) ConfigCursor(ctx context.Context) (configrepo.SyncCursor, error) {
	var cursor configrepo.SyncCursor
	_, err := s.getJSON(ctx, "config_cursor", &cursor)
	return cursor, err
}

// SaveConfigCursor persists the config sync cursor.
func (s *Store) SaveConfigCursor(ctx context.Context, cursor configrepo.SyncCursor) error {
	return s.putJSON(ctx, "config_cursor", cursor)
}

// UpsertApp creates or updates an app snapshot.
func (s *Store) UpsertApp(ctx context.Context, app configrepo.AppSnapshot) error {
	data, err := json.Marshal(app)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `insert into apps(name, payload) values(?, ?) on conflict(name) do update set payload = excluded.payload`, app.Name, string(data))
	return err
}

// App returns one app snapshot.
func (s *Store) App(ctx context.Context, name string) (configrepo.AppSnapshot, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select payload from apps where name = ?`, name).Scan(&raw)
	if err == sql.ErrNoRows {
		return configrepo.AppSnapshot{}, false, nil
	}
	if err != nil {
		return configrepo.AppSnapshot{}, false, err
	}
	var app configrepo.AppSnapshot
	if err := json.Unmarshal([]byte(raw), &app); err != nil {
		return configrepo.AppSnapshot{}, false, err
	}
	return app, true, nil
}

// ListApps returns all app snapshots.
func (s *Store) ListApps(ctx context.Context) ([]configrepo.AppSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `select payload from apps order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var apps []configrepo.AppSnapshot
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var app configrepo.AppSnapshot
		if err := json.Unmarshal([]byte(raw), &app); err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

// SaveImageCursor creates or updates an image cursor.
func (s *Store) SaveImageCursor(ctx context.Context, cursor imagewatch.Cursor) error {
	data, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	key := cursor.App + "/" + cursor.Service
	_, err = s.db.ExecContext(ctx, `insert into image_cursors(key, payload) values(?, ?) on conflict(key) do update set payload = excluded.payload`, key, string(data))
	return err
}

// ListImageCursors returns all image cursors.
func (s *Store) ListImageCursors(ctx context.Context) ([]imagewatch.Cursor, error) {
	rows, err := s.db.QueryContext(ctx, `select payload from image_cursors order by key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cursors []imagewatch.Cursor
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cursor imagewatch.Cursor
		if err := json.Unmarshal([]byte(raw), &cursor); err != nil {
			return nil, err
		}
		cursors = append(cursors, cursor)
	}
	return cursors, rows.Err()
}

// CreateRelease persists a release record.
func (s *Store) CreateRelease(ctx context.Context, record release.Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `insert into releases(id, app, created_at, payload) values(?, ?, ?, ?) on conflict(id) do update set payload = excluded.payload`, record.ID, record.App, record.CreatedAt.UnixNano(), string(data))
	return err
}

// UpdateReleaseStatus changes a release status.
func (s *Store) UpdateReleaseStatus(ctx context.Context, id string, status release.Status) error {
	record, ok, err := s.Release(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("release %s not found", id)
	}
	record.Status = status
	return s.CreateRelease(ctx, record)
}

// Release returns one release record.
func (s *Store) Release(ctx context.Context, id string) (release.Record, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select payload from releases where id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return release.Record{}, false, nil
	}
	if err != nil {
		return release.Record{}, false, err
	}
	var record release.Record
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return release.Record{}, false, err
	}
	return record, true, nil
}

// ListReleases returns release records for one app.
func (s *Store) ListReleases(ctx context.Context, app string) ([]release.Record, error) {
	query := `select payload from releases where app = ? order by created_at desc`
	args := []any{app}
	if app == "" {
		query = `select payload from releases order by created_at desc`
		args = nil
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []release.Record
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record release.Record
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// CreateDeployment persists a deployment attempt.
func (s *Store) CreateDeployment(ctx context.Context, record deployment.Record) error {
	return s.putDeployment(ctx, record)
}

// FinishDeployment records deployment completion.
func (s *Store) FinishDeployment(ctx context.Context, id string, status deployment.Status, errorMessage string, finishedAt time.Time) error {
	record, ok, err := s.deployment(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("deployment %s not found", id)
	}
	record.Status = status
	record.ErrorMessage = errorMessage
	record.FinishedAt = &finishedAt
	return s.putDeployment(ctx, record)
}

// ListDeployments returns deployments for one app.
func (s *Store) ListDeployments(ctx context.Context, app string) ([]deployment.Record, error) {
	query := `select payload from deployments where app = ? order by started_at desc`
	args := []any{app}
	if app == "" {
		query = `select payload from deployments order by started_at desc`
		args = nil
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []deployment.Record
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record deployment.Record
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// AddAuditEvent persists an audit event.
func (s *Store) AddAuditEvent(ctx context.Context, event audit.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `insert into audit_events(id, created_at, payload) values(?, ?, ?)`, event.ID, event.CreatedAt.UnixNano(), string(data))
	return err
}

// ListAuditEvents returns recent audit events.
func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]audit.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `select payload from audit_events order by created_at desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []audit.Event
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event audit.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
create table if not exists kv (
	key text primary key,
	value text not null
);
create table if not exists apps (
	name text primary key,
	payload text not null
);
create table if not exists image_cursors (
	key text primary key,
	payload text not null
);
create table if not exists releases (
	id text primary key,
	app text not null,
	created_at integer not null,
	payload text not null
);
create index if not exists releases_app_created_at on releases(app, created_at desc);
create table if not exists deployments (
	id text primary key,
	app text not null,
	started_at integer not null,
	payload text not null
);
create index if not exists deployments_app_started_at on deployments(app, started_at desc);
create table if not exists audit_events (
	id text primary key,
	created_at integer not null,
	payload text not null
);
create index if not exists audit_events_created_at on audit_events(created_at desc);
`)
	return err
}

func (s *Store) getJSON(ctx context.Context, key string, target any) (bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select value from kv where key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), target)
}

func (s *Store) putJSON(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `insert into kv(key, value) values(?, ?) on conflict(key) do update set value = excluded.value`, key, string(data))
	return err
}

func (s *Store) deployment(ctx context.Context, id string) (deployment.Record, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select payload from deployments where id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return deployment.Record{}, false, nil
	}
	if err != nil {
		return deployment.Record{}, false, err
	}
	var record deployment.Record
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return deployment.Record{}, false, err
	}
	return record, true, nil
}

func (s *Store) putDeployment(ctx context.Context, record deployment.Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `insert into deployments(id, app, started_at, payload) values(?, ?, ?, ?) on conflict(id) do update set payload = excluded.payload`, record.ID, record.App, record.StartedAt.UnixNano(), string(data))
	return err
}

func errorsJoin(first error, second error) error {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return fmt.Errorf("%w; %v", first, second)
}
