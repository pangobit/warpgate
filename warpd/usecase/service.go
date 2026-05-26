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

const maxLogTail = 2000

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
	now := s.now()
	settings, err := s.attachedRepositorySettings(ctx)
	if err != nil {
		return release.Record{}, err
	}
	snapshot, err := s.requiredApp(ctx, appName)
	if err != nil {
		return release.Record{}, err
	}
	preview, err := s.PreviewDeployData(ctx, appName, changes)
	if err != nil {
		return release.Record{}, err
	}
	artifacts, err := releaseCommitArtifacts(snapshot, preview, now)
	if err != nil {
		return release.Record{}, err
	}
	written, err := s.writeReleaseCommit(ctx, settings, snapshot, preview, artifacts)
	if err != nil {
		return release.Record{}, err
	}
	record := committedReleaseRecord(actor, snapshot, preview, artifacts, written, now)
	snapshot = committedAppSnapshot(snapshot, preview.YAML, written, now)
	if err := s.store.UpsertApp(ctx, snapshot); err != nil {
		return release.Record{}, err
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
	configInput, err := s.runtimeConfig(ctx)
	if err != nil {
		return deployment.Record{}, err
	}
	settings, err := s.attachedRepositorySettings(ctx)
	if err != nil {
		return deployment.Record{}, err
	}
	configInput.Apps, err = s.resolveRuntimeAppCompose(ctx, settings, configInput.Apps, record.App)
	if err != nil {
		return deployment.Record{}, err
	}
	releaseManifests, err := s.releaseManifests(ctx, record.App, record)
	if err != nil {
		return deployment.Record{}, err
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
		App:              record.App,
		ReleaseID:        record.ID,
		ConfigCommit:     record.ConfigCommit,
		ManifestJSON:     record.ManifestJSON,
		Config:           configInput,
		ReleaseManifests: releaseManifests,
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

func (s *Service) releaseManifests(ctx context.Context, app string, current release.Record) ([]ReleaseManifestInput, error) {
	records, err := s.store.ListReleases(ctx, app)
	if err != nil {
		return nil, err
	}
	manifests := make([]ReleaseManifestInput, 0, len(records)+1)
	seen := make(map[string]bool, len(records)+1)
	for _, record := range records {
		if record.ManifestJSON == "" {
			continue
		}
		manifests = append(manifests, ReleaseManifestInput{
			ID:           record.ID,
			ManifestJSON: record.ManifestJSON,
		})
		seen[record.ID] = true
	}
	if current.ManifestJSON != "" && !seen[current.ID] {
		manifests = append(manifests, ReleaseManifestInput{
			ID:           current.ID,
			ManifestJSON: current.ManifestJSON,
		})
	}
	return manifests, nil
}

// ConfigNodes returns cluster nodes from the local desired-state config.
func (s *Service) ConfigNodes(ctx context.Context, actor identity.User) ([]ConfigNode, error) {
	if err := identity.RequireAdmin(actor); err != nil {
		return nil, err
	}
	input, err := s.runtimeConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.deployer.ConfigNodes(ctx, input)
}

// RuntimeStatus returns live cluster status from deployment targets.
func (s *Service) RuntimeStatus(ctx context.Context, actor identity.User) (RuntimeStatus, error) {
	if err := identity.RequireAdmin(actor); err != nil {
		return RuntimeStatus{}, err
	}
	input, err := s.runtimeConfig(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	return s.deployer.RuntimeStatus(ctx, input)
}

// AppRuntimeStatus returns live status for one app across target nodes.
func (s *Service) AppRuntimeStatus(ctx context.Context, actor identity.User, app string) ([]RuntimeNodeStatus, error) {
	if err := identity.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(app) == "" {
		return nil, fmt.Errorf("app is required")
	}
	input, err := s.runtimeConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.deployer.AppRuntimeStatus(ctx, input, app)
}

// Logs fetches live container logs from a deployment target.
func (s *Service) Logs(ctx context.Context, actor identity.User, input LogsInput) (LogsResult, error) {
	if err := identity.RequireAdmin(actor); err != nil {
		return LogsResult{}, err
	}
	input.NodeID = strings.TrimSpace(input.NodeID)
	input.App = strings.TrimSpace(input.App)
	input.Grep = strings.TrimSpace(input.Grep)
	if input.NodeID == "" {
		return LogsResult{}, fmt.Errorf("node is required")
	}
	if input.Tail <= 0 {
		input.Tail = 100
	}
	if input.Tail > maxLogTail {
		return LogsResult{}, fmt.Errorf("tail must be %d or less", maxLogTail)
	}
	configInput, err := s.runtimeConfig(ctx)
	if err != nil {
		return LogsResult{}, err
	}
	return s.deployer.Logs(ctx, configInput, input)
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
	parsedApp, err := parseApp(snapshot.Name, snapshot.RawYAML)
	if err != nil {
		return AppDetail{}, err
	}
	return AppDetail{
		App:         snapshot,
		Services:    releaseServices(parsedApp),
		Releases:    releases,
		Deployments: deployments,
	}, nil
}

// AppDetail is the web app detail view model.
type AppDetail struct {
	// App is the desired-state snapshot.
	App configrepo.AppSnapshot
	// Services are the release services declared by app.yml.
	Services []AppReleaseService
	// Releases are the app release records.
	Releases []release.Record
	// Deployments are the app deployment records.
	Deployments []deployment.Record
}

// AppReleaseService describes an editable app release service.
type AppReleaseService struct {
	// Name is the release service name.
	Name string
	// Image is the service image repository.
	Image string
	// ImageTag is the configured image tag.
	ImageTag string
	// ImageDigest is the configured image digest.
	ImageDigest string
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
	if err := s.store.SaveClusterConfig(ctx, configrepo.ClusterSnapshot{
		Path:         clusterFile.Path,
		ConfigCommit: ref,
		FileSHA:      clusterFile.SHA,
		RawYAML:      clusterFile.Content,
		UpdatedAt:    observedAt,
	}); err != nil {
		return err
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
		composeContent, err := s.appComposeContent(ctx, settings, app, appName, ref)
		if err != nil {
			return err
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

func (s *Service) appComposeContent(ctx context.Context, settings configrepo.RepositorySettings, app *config.AppConfig, appName string, ref string) (string, error) {
	if app.Source != nil {
		sourceSettings, err := sourceRepositorySettings(app.Source, app.ComposeRef)
		if err != nil {
			return "", fmt.Errorf("app %s: %w", appName, err)
		}
		composePath, err := sourceComposePath(app.Source)
		if err != nil {
			return "", fmt.Errorf("app %s: %w", appName, err)
		}
		compose, err := s.github.ReadFile(ctx, sourceSettings, composePath, app.ComposeRef)
		if err != nil {
			return "", fmt.Errorf("app %s: read source compose: %w", appName, err)
		}
		return compose.Content, nil
	}
	composePath := repositoryPath(settings, path.Join("apps", appName, "compose.yml"))
	compose, err := s.github.ReadFile(ctx, settings, composePath, ref)
	if err != nil {
		return "", fmt.Errorf("app %s: compose.yml is required when source is not set", appName)
	}
	return compose.Content, nil
}

func sourceRepositorySettings(source *config.SourceConfig, ref string) (configrepo.RepositorySettings, error) {
	repo := strings.TrimPrefix(source.Repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	parts := strings.Split(repo, "/")
	if len(parts) < 2 {
		return configrepo.RepositorySettings{}, fmt.Errorf("invalid source repo %q", source.Repo)
	}
	return configrepo.RepositorySettings{
		Owner:  parts[len(parts)-2],
		Repo:   parts[len(parts)-1],
		Branch: ref,
	}, nil
}

func sourceComposePath(source *config.SourceConfig) (string, error) {
	if source.ComposePath == "" {
		return "compose.yml", nil
	}
	cleaned := path.Clean(source.ComposePath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid source compose path %q", source.ComposePath)
	}
	return cleaned, nil
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

func githubFileByPath(files []GitHubFile, target string) (GitHubFile, bool) {
	for _, file := range files {
		if file.Path == target {
			return file, true
		}
	}
	return GitHubFile{}, false
}

type releaseCommitArtifactSet struct {
	manifest        *pkgrelease.Manifest
	manifestContent string
	files           []WriteFileChange
}

func releaseCommitArtifacts(snapshot configrepo.AppSnapshot, preview release.CommitPreview, now time.Time) (releaseCommitArtifactSet, error) {
	app, err := parseApp(snapshot.Name, preview.YAML)
	if err != nil {
		return releaseCommitArtifactSet{}, err
	}
	manifest := pkgrelease.Build(app, []byte(snapshot.ComposeYAML), now)
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return releaseCommitArtifactSet{}, fmt.Errorf("marshal release manifest: %w", err)
	}
	manifestContent := string(append(manifestJSON, '\n'))
	releaseDir := path.Join(path.Dir(snapshot.Path), "releases")
	return releaseCommitArtifactSet{
		manifest:        manifest,
		manifestContent: manifestContent,
		files: []WriteFileChange{
			{Path: snapshot.Path, Content: preview.YAML},
			{Path: path.Join(releaseDir, manifest.ID+".json"), Content: manifestContent},
			{Path: path.Join(releaseDir, "latest.json"), Content: manifestContent},
		},
	}, nil
}

func (s *Service) attachedRepositorySettings(ctx context.Context) (configrepo.RepositorySettings, error) {
	settings, ok, err := s.store.RepositorySettings(ctx)
	if err != nil {
		return configrepo.RepositorySettings{}, err
	}
	if !ok {
		return configrepo.RepositorySettings{}, fmt.Errorf("repository is not attached")
	}
	return settings, nil
}

func (s *Service) requiredApp(ctx context.Context, appName string) (configrepo.AppSnapshot, error) {
	snapshot, ok, err := s.store.App(ctx, appName)
	if err != nil {
		return configrepo.AppSnapshot{}, err
	}
	if !ok {
		return configrepo.AppSnapshot{}, fmt.Errorf("app %s not found", appName)
	}
	return snapshot, nil
}

func (s *Service) writeReleaseCommit(ctx context.Context, settings configrepo.RepositorySettings, snapshot configrepo.AppSnapshot, preview release.CommitPreview, artifacts releaseCommitArtifactSet) (GitHubFile, error) {
	latest, err := s.github.ReadFile(ctx, settings, snapshot.Path, settings.Branch)
	if err != nil {
		return GitHubFile{}, err
	}
	if latest.SHA != snapshot.FileSHA {
		return GitHubFile{}, ErrConflict
	}
	writtenFiles, err := s.github.WriteFiles(ctx, WriteFilesInput{
		Settings: settings,
		Message:  preview.Message,
		Files:    artifacts.files,
	})
	if err != nil {
		return GitHubFile{}, err
	}
	written, ok := githubFileByPath(writtenFiles, snapshot.Path)
	if !ok {
		return GitHubFile{}, fmt.Errorf("github write did not return %s", snapshot.Path)
	}
	return written, nil
}

func committedReleaseRecord(actor identity.User, snapshot configrepo.AppSnapshot, preview release.CommitPreview, artifacts releaseCommitArtifactSet, written GitHubFile, now time.Time) release.Record {
	return release.Record{
		ID:           artifacts.manifest.ID,
		App:          snapshot.Name,
		ConfigCommit: written.CommitSHA,
		ManifestJSON: artifacts.manifestContent,
		RawYAML:      preview.YAML,
		Status:       release.StatusReady,
		ActorEmail:   actor.Email,
		CreatedAt:    now,
	}
}

func committedAppSnapshot(snapshot configrepo.AppSnapshot, rawYAML string, written GitHubFile, now time.Time) configrepo.AppSnapshot {
	snapshot.RawYAML = rawYAML
	snapshot.FileSHA = written.SHA
	snapshot.ConfigCommit = written.CommitSHA
	snapshot.UpdatedAt = now
	return snapshot
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

func (s *Service) deployRepositoryPath(ctx context.Context) (string, error) {
	settings, ok, err := s.store.RepositorySettings(ctx)
	if err != nil || !ok {
		return "", err
	}
	return settings.Path, nil
}

func (s *Service) runtimeConfig(ctx context.Context) (RuntimeConfigInput, error) {
	settings, err := s.attachedRepositorySettings(ctx)
	if err != nil {
		return RuntimeConfigInput{}, err
	}
	cluster, ok, err := s.store.ClusterConfig(ctx)
	if err != nil {
		return RuntimeConfigInput{}, err
	}
	if !ok {
		return RuntimeConfigInput{}, fmt.Errorf("cluster config has not been synced")
	}
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return RuntimeConfigInput{}, err
	}
	return RuntimeConfigInput{
		RepositoryPath: settings.Path,
		Cluster:        cluster,
		Apps:           apps,
	}, nil
}

func (s *Service) resolveRuntimeAppCompose(ctx context.Context, settings configrepo.RepositorySettings, apps []configrepo.AppSnapshot, appName string) ([]configrepo.AppSnapshot, error) {
	resolved := make([]configrepo.AppSnapshot, 0, len(apps))
	for _, snapshot := range apps {
		if snapshot.Name != appName {
			resolved = append(resolved, snapshot)
			continue
		}
		next, err := s.resolveRuntimeAppSnapshotCompose(ctx, settings, snapshot)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, next)
	}
	return resolved, nil
}

func (s *Service) resolveRuntimeAppSnapshotCompose(ctx context.Context, settings configrepo.RepositorySettings, snapshot configrepo.AppSnapshot) (configrepo.AppSnapshot, error) {
	if snapshot.ComposeYAML != "" {
		return snapshot, nil
	}
	app, err := parseApp(snapshot.Name, snapshot.RawYAML)
	if err != nil {
		return configrepo.AppSnapshot{}, err
	}
	if app.Source == nil {
		return snapshot, nil
	}
	composeContent, err := s.appComposeContent(ctx, settings, app, snapshot.Name, snapshot.ConfigCommit)
	if err != nil {
		return configrepo.AppSnapshot{}, err
	}
	snapshot.ComposeYAML = composeContent
	snapshot.UpdatedAt = s.now()
	if err := s.store.UpsertApp(ctx, snapshot); err != nil {
		return configrepo.AppSnapshot{}, err
	}
	return snapshot, nil
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

func releaseServices(app *config.AppConfig) []AppReleaseService {
	names := make([]string, 0, len(app.Release.Services))
	for name := range app.Release.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	services := make([]AppReleaseService, 0, len(names))
	for _, name := range names {
		service := app.Release.Services[name]
		services = append(services, AppReleaseService{
			Name:        name,
			Image:       service.Image,
			ImageTag:    service.ImageTag,
			ImageDigest: service.ImageDigest,
		})
	}
	return services
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
