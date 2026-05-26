package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pangobit/warpgate/pkg/config"
	pkgrelease "github.com/pangobit/warpgate/pkg/release"
	"github.com/pangobit/warpgate/warpd/internal/audit"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/deployment"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"gopkg.in/yaml.v3"
)

// ErrConflict indicates that the desired-state repository moved before a write.
var ErrConflict = errors.New("repository conflict: refresh before retrying")

// Service coordinates Warpgate web workflows.
type Service struct {
	store    Store
	github   GitHubRepo
	registry Registry
	deployer Deployer
	now      func() time.Time
}

// NewService creates a Warpgate application service.
func NewService(store Store, github GitHubRepo, registry Registry, deployer Deployer) *Service {
	return &Service{
		store:    store,
		github:   github,
		registry: registry,
		deployer: deployer,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// AttachRepository validates and persists an existing GitHub infra repo.
func (s *Service) AttachRepository(ctx context.Context, actor identity.User, settings configrepo.RepositorySettings) error {
	if err := identity.RequireAdmin(actor); err != nil {
		return err
	}
	if settings.Owner == "" || settings.Repo == "" || settings.Branch == "" {
		return fmt.Errorf("owner, repo, and branch are required")
	}
	rootPath, err := cleanRepositoryPath(settings.Path)
	if err != nil {
		return err
	}
	settings.Path = rootPath
	if settings.AttachedAt.IsZero() {
		settings.AttachedAt = s.now()
	}
	head, err := s.github.BranchHead(ctx, settings)
	if err != nil {
		return fmt.Errorf("read branch head: %w", err)
	}
	if err := s.validateRepoAtRef(ctx, settings, head); err != nil {
		return err
	}
	if err := s.store.SaveRepositorySettings(ctx, settings); err != nil {
		return err
	}
	if err := s.SyncConfig(ctx, actor); err != nil {
		return err
	}
	return s.audit(ctx, "repo.attach", actor.Email, settings.Owner+"/"+settings.Repo+" attached")
}

// SyncConfig imports the latest desired state from the attached repository.
func (s *Service) SyncConfig(ctx context.Context, actor identity.User) error {
	settings, ok, err := s.store.RepositorySettings(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("repository is not attached")
	}
	head, err := s.github.BranchHead(ctx, settings)
	if err != nil {
		return s.recordConfigSyncError(ctx, err)
	}
	cursor, err := s.store.ConfigCursor(ctx)
	if err != nil {
		return err
	}
	checkedAt := s.now()
	if cursor.LastObservedCommit == head {
		cursor.LastCheckedAt = checkedAt
		cursor.LastError = ""
		return s.store.SaveConfigCursor(ctx, cursor)
	}
	if err := s.importRepoAtRef(ctx, settings, head, checkedAt); err != nil {
		return s.recordConfigSyncError(ctx, err)
	}
	cursor.LastObservedCommit = head
	cursor.LastCheckedAt = checkedAt
	cursor.LastError = ""
	if err := s.store.SaveConfigCursor(ctx, cursor); err != nil {
		return err
	}
	return s.audit(ctx, "config.sync", actor.Email, "config synced at "+head)
}

// CheckImages polls mutable image tags for all observed apps.
func (s *Service) CheckImages(ctx context.Context, actor identity.User) error {
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return err
	}
	existing, err := s.store.ListImageCursors(ctx)
	if err != nil {
		return err
	}
	prior := make(map[string]imagewatch.Cursor, len(existing))
	for _, cursor := range existing {
		prior[cursor.App+"/"+cursor.Service] = cursor
	}
	for _, snapshot := range apps {
		app, err := parseApp(snapshot.Name, snapshot.RawYAML)
		if err != nil {
			return err
		}
		for serviceName, service := range app.Release.Services {
			if service.ImageDigest != "" {
				continue
			}
			tag := service.EffectiveImageTag()
			cursor := prior[snapshot.Name+"/"+serviceName]
			cursor.App = snapshot.Name
			cursor.Service = serviceName
			cursor.Image = service.Image
			cursor.Tag = tag
			cursor.LastCheckedAt = s.now()
			digest, err := s.registry.ResolveDigest(ctx, service.Image, tag)
			if err != nil {
				cursor.Status = imagewatch.StatusInvalid
				cursor.LastError = err.Error()
				if saveErr := s.store.SaveImageCursor(ctx, cursor); saveErr != nil {
					return saveErr
				}
				continue
			}
			cursor.LastError = ""
			if cursor.LastDigest != "" && cursor.LastDigest != digest {
				cursor.PreviousDigest = cursor.LastDigest
				cursor.Status = imagewatch.StatusChanged
			} else {
				cursor.Status = imagewatch.StatusReady
			}
			cursor.LastDigest = digest
			if err := s.store.SaveImageCursor(ctx, cursor); err != nil {
				return err
			}
		}
	}
	return s.audit(ctx, "images.sync", actor.Email, "image metadata checked")
}

// PreviewDeployData validates a deploy-data edit and returns the YAML and commit preview.
func (s *Service) PreviewDeployData(ctx context.Context, appName string, changes []release.DeployDataChange) (release.CommitPreview, error) {
	snapshot, ok, err := s.store.App(ctx, appName)
	if err != nil {
		return release.CommitPreview{}, err
	}
	if !ok {
		return release.CommitPreview{}, fmt.Errorf("app %s not found", appName)
	}
	app, err := applyDeployData(snapshot.Name, snapshot.RawYAML, changes)
	if err != nil {
		return release.CommitPreview{}, err
	}
	raw, err := marshalApp(app)
	if err != nil {
		return release.CommitPreview{}, err
	}
	return release.CommitPreview{
		Path:    snapshot.Path,
		Message: commitMessage(app.Name, changes),
		YAML:    raw,
	}, nil
}

// CommitRelease writes deploy data to GitHub and creates a committed release record.
func (s *Service) CommitRelease(ctx context.Context, actor identity.User, appName string, changes []release.DeployDataChange) (release.Record, error) {
	if err := identity.RequireAdmin(actor); err != nil {
		return release.Record{}, err
	}
	settings, ok, err := s.store.RepositorySettings(ctx)
	if err != nil {
		return release.Record{}, err
	}
	if !ok {
		return release.Record{}, fmt.Errorf("repository is not attached")
	}
	snapshot, ok, err := s.store.App(ctx, appName)
	if err != nil {
		return release.Record{}, err
	}
	if !ok {
		return release.Record{}, fmt.Errorf("app %s not found", appName)
	}
	preview, err := s.PreviewDeployData(ctx, appName, changes)
	if err != nil {
		return release.Record{}, err
	}
	latest, err := s.github.ReadFile(ctx, settings, snapshot.Path, settings.Branch)
	if err != nil {
		return release.Record{}, err
	}
	if latest.SHA != snapshot.FileSHA {
		return release.Record{}, ErrConflict
	}
	written, err := s.github.WriteFile(ctx, WriteFileInput{
		Settings:    settings,
		Path:        snapshot.Path,
		Content:     preview.YAML,
		ExpectedSHA: snapshot.FileSHA,
		Message:     preview.Message,
	})
	if err != nil {
		return release.Record{}, err
	}
	snapshot.RawYAML = preview.YAML
	snapshot.FileSHA = written.SHA
	snapshot.ConfigCommit = written.CommitSHA
	snapshot.UpdatedAt = s.now()
	if err := s.store.UpsertApp(ctx, snapshot); err != nil {
		return release.Record{}, err
	}
	app, err := parseApp(snapshot.Name, snapshot.RawYAML)
	if err != nil {
		return release.Record{}, err
	}
	manifest := pkgrelease.Build(app, []byte(snapshot.ComposeYAML), s.now())
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return release.Record{}, fmt.Errorf("marshal release manifest: %w", err)
	}
	record := release.Record{
		ID:           manifest.ID,
		App:          snapshot.Name,
		ConfigCommit: snapshot.ConfigCommit,
		ManifestJSON: string(manifestJSON),
		RawYAML:      snapshot.RawYAML,
		Status:       release.StatusReady,
		ActorEmail:   actor.Email,
		CreatedAt:    s.now(),
	}
	if err := s.store.CreateRelease(ctx, record); err != nil {
		return release.Record{}, err
	}
	if err := s.audit(ctx, "release.commit", actor.Email, preview.Message); err != nil {
		return release.Record{}, err
	}
	return record, nil
}

// DeployRelease deploys a committed release and persists the attempt.
func (s *Service) DeployRelease(ctx context.Context, actor identity.User, releaseID string) (deployment.Record, error) {
	if err := identity.RequireAdmin(actor); err != nil {
		return deployment.Record{}, err
	}
	record, ok, err := s.store.Release(ctx, releaseID)
	if err != nil {
		return deployment.Record{}, err
	}
	if !ok {
		return deployment.Record{}, fmt.Errorf("release %s not found", releaseID)
	}
	startedAt := s.now()
	deploy := deployment.Record{
		ID:         newID("dep"),
		ReleaseID:  record.ID,
		App:        record.App,
		ActorEmail: actor.Email,
		Status:     deployment.StatusRunning,
		StartedAt:  startedAt,
		Targets:    nil,
	}
	if err := s.store.CreateDeployment(ctx, deploy); err != nil {
		return deployment.Record{}, err
	}
	if err := s.store.UpdateReleaseStatus(ctx, record.ID, release.StatusDeploying); err != nil {
		return deployment.Record{}, err
	}
	result, deployErr := s.deployer.DeployRelease(ctx, DeployReleaseInput{
		App:          record.App,
		ReleaseID:    record.ID,
		ConfigCommit: record.ConfigCommit,
	})
	finishedAt := s.now()
	deploy.Targets = result.Targets
	if deployErr != nil {
		deploy.Status = deployment.StatusFailed
		deploy.ErrorMessage = deployErr.Error()
		statusErr := s.store.UpdateReleaseStatus(ctx, record.ID, release.StatusFailed)
		if err := s.store.FinishDeployment(ctx, deploy.ID, deployment.StatusFailed, deployErr.Error(), finishedAt); err != nil {
			return deployment.Record{}, err
		}
		auditErr := s.audit(ctx, "deploy.failed", actor.Email, deployErr.Error())
		if statusErr != nil || auditErr != nil {
			return deployment.Record{}, errors.Join(deployErr, statusErr, auditErr)
		}
		return deploy, deployErr
	}
	deploy.Status = deployment.StatusSucceeded
	deploy.FinishedAt = &finishedAt
	if err := s.store.FinishDeployment(ctx, deploy.ID, deployment.StatusSucceeded, "", finishedAt); err != nil {
		return deployment.Record{}, err
	}
	if err := s.store.UpdateReleaseStatus(ctx, record.ID, release.StatusDeployed); err != nil {
		return deployment.Record{}, err
	}
	return deploy, s.audit(ctx, "deploy.succeeded", actor.Email, record.App+" "+record.ID)
}

// Dashboard returns the aggregate web dashboard state.
func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	settings, attached, err := s.store.RepositorySettings(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	cursor, err := s.store.ConfigCursor(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	images, err := s.store.ListImageCursors(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	var changed int
	for _, cursor := range images {
		if cursor.Status == imagewatch.StatusChanged {
			changed++
		}
	}
	return Dashboard{
		RepositoryAttached: attached,
		Repository:         settings,
		ConfigCursor:       cursor,
		AppCount:           len(apps),
		ImageUpdates:       changed,
	}, nil
}

// Dashboard is the web dashboard view model.
type Dashboard struct {
	// RepositoryAttached reports whether a repo is configured.
	RepositoryAttached bool
	// Repository is the attached repo settings.
	Repository configrepo.RepositorySettings
	// ConfigCursor is the latest config sync state.
	ConfigCursor configrepo.SyncCursor
	// AppCount is the number of observed apps.
	AppCount int
	// ImageUpdates is the count of watched images with digest changes.
	ImageUpdates int
}

// Apps returns observed apps sorted by name.
func (s *Service) Apps(ctx context.Context) ([]configrepo.AppSnapshot, error) {
	return s.store.ListApps(ctx)
}

// RepositorySettings returns the attached repository settings.
func (s *Service) RepositorySettings(ctx context.Context) (configrepo.RepositorySettings, bool, error) {
	return s.store.RepositorySettings(ctx)
}

// Release returns one release record.
func (s *Service) Release(ctx context.Context, id string) (release.Record, bool, error) {
	return s.store.Release(ctx, id)
}

// PollerSettings returns the persisted poller settings.
func (s *Service) PollerSettings(ctx context.Context) (configrepo.PollerSettings, error) {
	return s.store.PollerSettings(ctx)
}

// AppDetail returns one app and its operational history.
func (s *Service) AppDetail(ctx context.Context, app string) (AppDetail, error) {
	snapshot, ok, err := s.store.App(ctx, app)
	if err != nil {
		return AppDetail{}, err
	}
	if !ok {
		return AppDetail{}, fmt.Errorf("app %s not found", app)
	}
	releases, err := s.store.ListReleases(ctx, app)
	if err != nil {
		return AppDetail{}, err
	}
	deployments, err := s.store.ListDeployments(ctx, app)
	if err != nil {
		return AppDetail{}, err
	}
	return AppDetail{App: snapshot, Releases: releases, Deployments: deployments}, nil
}

// AppDetail is the web app detail view model.
type AppDetail struct {
	// App is the desired-state snapshot.
	App configrepo.AppSnapshot
	// Releases are the app release records.
	Releases []release.Record
	// Deployments are the app deployment records.
	Deployments []deployment.Record
}

func (s *Service) validateRepoAtRef(ctx context.Context, settings configrepo.RepositorySettings, ref string) error {
	return s.importRepoAtRef(ctx, settings, ref, s.now())
}

func (s *Service) importRepoAtRef(ctx context.Context, settings configrepo.RepositorySettings, ref string, observedAt time.Time) error {
	clusterFile, err := s.github.ReadFile(ctx, settings, repositoryPath(settings, "cluster.yml"), ref)
	if err != nil {
		return fmt.Errorf("read cluster.yml: %w", err)
	}
	var cluster config.ClusterConfig
	if err := yaml.Unmarshal([]byte(clusterFile.Content), &cluster); err != nil {
		return fmt.Errorf("parse cluster.yml: %w", err)
	}
	if err := cluster.Validate(); err != nil {
		return fmt.Errorf("invalid cluster.yml: %w", err)
	}
	appFiles, err := s.github.ListAppConfigFiles(ctx, settings, ref)
	if err != nil {
		return fmt.Errorf("list app configs: %w", err)
	}
	for _, file := range appFiles {
		appName := path.Base(path.Dir(file.Path))
		app, err := parseApp(appName, file.Content)
		if err != nil {
			return err
		}
		composePath := repositoryPath(settings, path.Join("apps", appName, "compose.yml"))
		compose, err := s.github.ReadFile(ctx, settings, composePath, ref)
		composeContent := ""
		if err == nil {
			composeContent = compose.Content
		}
		if app.Source == nil && composeContent == "" {
			return fmt.Errorf("app %s: compose.yml is required when source is not set", appName)
		}
		if err := s.store.UpsertApp(ctx, configrepo.AppSnapshot{
			Name:         appName,
			Path:         file.Path,
			ConfigCommit: ref,
			FileSHA:      file.SHA,
			RawYAML:      file.Content,
			ComposeYAML:  composeContent,
			UpdatedAt:    observedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func cleanRepositoryPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "/")
	if value == "" || value == "." {
		return "", nil
	}
	cleaned := path.Clean(value)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("repository path must stay inside the repository")
	}
	return cleaned, nil
}

func repositoryPath(settings configrepo.RepositorySettings, relative string) string {
	if settings.Path == "" {
		return relative
	}
	return path.Join(settings.Path, relative)
}

func (s *Service) recordConfigSyncError(ctx context.Context, cause error) error {
	cursor, err := s.store.ConfigCursor(ctx)
	if err != nil {
		return err
	}
	cursor.LastCheckedAt = s.now()
	cursor.LastError = cause.Error()
	if err := s.store.SaveConfigCursor(ctx, cursor); err != nil {
		return err
	}
	return cause
}

func (s *Service) audit(ctx context.Context, eventType, actorEmail, message string) error {
	return s.store.AddAuditEvent(ctx, audit.Event{
		ID:         newID("evt"),
		Type:       eventType,
		ActorEmail: actorEmail,
		Message:    message,
		CreatedAt:  s.now(),
	})
}

func parseApp(name string, raw string) (*config.AppConfig, error) {
	var app config.AppConfig
	if err := yaml.Unmarshal([]byte(raw), &app); err != nil {
		return nil, fmt.Errorf("parse app %s: %w", name, err)
	}
	app.Name = name
	if app.Kind != "" && app.Kind != config.AppKind {
		return nil, fmt.Errorf("app %s: unknown kind %q", name, app.Kind)
	}
	if err := config.ValidateApp(&app); err != nil {
		return nil, err
	}
	return &app, nil
}

func applyDeployData(name string, raw string, changes []release.DeployDataChange) (*config.AppConfig, error) {
	app, err := parseApp(name, raw)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		if change.Service == "" {
			return nil, fmt.Errorf("service is required")
		}
		service, ok := app.Release.Services[change.Service]
		if !ok {
			return nil, fmt.Errorf("release.services.%s not found", change.Service)
		}
		if change.ImageTag != "" {
			service.ImageTag = change.ImageTag
			service.ImageDigest = ""
		}
		if change.ImageDigest != "" {
			service.ImageDigest = change.ImageDigest
		}
		if change.Environment != nil {
			service.Environment = change.Environment
		}
		app.Release.Services[change.Service] = service
		if change.Targets != nil {
			app.Targets = change.Targets
		}
		if change.Strategy != "" {
			app.Strategy = config.DeployStrategy(change.Strategy)
		}
	}
	if err := config.ValidateApp(app); err != nil {
		return nil, err
	}
	return app, nil
}

func marshalApp(app *config.AppConfig) (string, error) {
	clone := *app
	clone.Name = ""
	data, err := yaml.Marshal(&clone)
	if err != nil {
		return "", fmt.Errorf("marshal app.yml: %w", err)
	}
	return string(data), nil
}

func commitMessage(app string, changes []release.DeployDataChange) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		value := change.ImageDigest
		if value == "" {
			value = change.ImageTag
		}
		if value == "" {
			value = "deploy-data"
		}
		parts = append(parts, change.Service+"="+value)
	}
	sort.Strings(parts)
	return "warpgate: release " + app + " " + strings.Join(parts, " ")
}

func newID(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "-" + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}
