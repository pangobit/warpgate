// Package usecase orchestrates Warpgate web workflows.
package usecase

import (
	"context"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/audit"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/deployment"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/internal/release"
)

// Store persists Warpgate operational state.
type Store interface {
	RepositorySettings(ctx context.Context) (configrepo.RepositorySettings, bool, error)
	SaveRepositorySettings(ctx context.Context, settings configrepo.RepositorySettings) error
	PollerSettings(ctx context.Context) (configrepo.PollerSettings, error)
	SavePollerSettings(ctx context.Context, settings configrepo.PollerSettings) error
	ConfigCursor(ctx context.Context) (configrepo.SyncCursor, error)
	SaveConfigCursor(ctx context.Context, cursor configrepo.SyncCursor) error
	UpsertApp(ctx context.Context, app configrepo.AppSnapshot) error
	App(ctx context.Context, name string) (configrepo.AppSnapshot, bool, error)
	ListApps(ctx context.Context) ([]configrepo.AppSnapshot, error)
	SaveImageCursor(ctx context.Context, cursor imagewatch.Cursor) error
	ListImageCursors(ctx context.Context) ([]imagewatch.Cursor, error)
	CreateRelease(ctx context.Context, record release.Record) error
	UpdateReleaseStatus(ctx context.Context, id string, status release.Status) error
	Release(ctx context.Context, id string) (release.Record, bool, error)
	ListReleases(ctx context.Context, app string) ([]release.Record, error)
	CreateDeployment(ctx context.Context, record deployment.Record) error
	FinishDeployment(ctx context.Context, id string, status deployment.Status, errorMessage string, finishedAt time.Time) error
	ListDeployments(ctx context.Context, app string) ([]deployment.Record, error)
	AddAuditEvent(ctx context.Context, event audit.Event) error
	ListAuditEvents(ctx context.Context, limit int) ([]audit.Event, error)
}

// GitHubRepo reads and writes the configured infrastructure repository.
type GitHubRepo interface {
	BranchHead(ctx context.Context, settings configrepo.RepositorySettings) (string, error)
	ReadFile(ctx context.Context, settings configrepo.RepositorySettings, path string, ref string) (GitHubFile, error)
	ListAppConfigFiles(ctx context.Context, settings configrepo.RepositorySettings, ref string) ([]GitHubFile, error)
	WriteFile(ctx context.Context, input WriteFileInput) (GitHubFile, error)
}

// GitHubFile is a repository file snapshot.
type GitHubFile struct {
	// Path is the repository path.
	Path string
	// Content is the decoded file content.
	Content string
	// SHA is the GitHub blob SHA.
	SHA string
	// CommitSHA is the commit that produced the content.
	CommitSHA string
}

// WriteFileInput describes an optimistic GitHub file write.
type WriteFileInput struct {
	// Settings identifies the target repository.
	Settings configrepo.RepositorySettings
	// Path is the repository path.
	Path string
	// Content is the new file content.
	Content string
	// ExpectedSHA is the blob SHA that must still be current.
	ExpectedSHA string
	// Message is the Git commit message.
	Message string
}

// Registry resolves mutable image tags to digests.
type Registry interface {
	ResolveDigest(ctx context.Context, image string, tag string) (string, error)
}

// Deployer executes a committed release.
type Deployer interface {
	DeployRelease(ctx context.Context, input DeployReleaseInput) (DeployResult, error)
}

// DeployReleaseInput is the deploy adapter input.
type DeployReleaseInput struct {
	// App is the app name.
	App string
	// ReleaseID is the release identifier.
	ReleaseID string
	// ConfigCommit is the desired-state commit SHA.
	ConfigCommit string
}

// DeployResult summarizes a deploy adapter result.
type DeployResult struct {
	// Targets are the nodes that were attempted.
	Targets []string
}
