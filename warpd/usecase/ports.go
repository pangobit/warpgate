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
	ClusterConfig(ctx context.Context) (configrepo.ClusterSnapshot, bool, error)
	SaveClusterConfig(ctx context.Context, cluster configrepo.ClusterSnapshot) error
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
	WriteFiles(ctx context.Context, input WriteFilesInput) ([]GitHubFile, error)
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

// WriteFilesInput describes a multi-file GitHub commit.
type WriteFilesInput struct {
	// Settings identifies the target repository.
	Settings configrepo.RepositorySettings
	// Files are the repository files to write.
	Files []WriteFileChange
	// Message is the Git commit message.
	Message string
}

// WriteFileChange describes one repository file change.
type WriteFileChange struct {
	// Path is the repository path.
	Path string
	// Content is the new file content.
	Content string
}

// Registry resolves mutable image tags to digests.
type Registry interface {
	ResolveDigest(ctx context.Context, image string, tag string) (string, error)
}

// Deployer executes a committed release.
type Deployer interface {
	DeployRelease(ctx context.Context, input DeployReleaseInput) (DeployResult, error)
	ConfigNodes(ctx context.Context, input RuntimeConfigInput) ([]ConfigNode, error)
	RuntimeStatus(ctx context.Context, input RuntimeConfigInput) (RuntimeStatus, error)
	AppRuntimeStatus(ctx context.Context, input RuntimeConfigInput, app string) ([]RuntimeNodeStatus, error)
	Logs(ctx context.Context, config RuntimeConfigInput, input LogsInput) (LogsResult, error)
}

// DeployReleaseInput is the deploy adapter input.
type DeployReleaseInput struct {
	// App is the app name.
	App string
	// ReleaseID is the release identifier.
	ReleaseID string
	// ConfigCommit is the desired-state commit SHA.
	ConfigCommit string
	// ManifestJSON is the persisted immutable release manifest.
	ManifestJSON string
	// Config is the synced desired-state config.
	Config RuntimeConfigInput
	// ReleaseManifests are known release manifests for the app.
	ReleaseManifests []ReleaseManifestInput
}

// ReleaseManifestInput is a release manifest available to the deploy adapter.
type ReleaseManifestInput struct {
	// ID is the release identifier.
	ID string
	// ManifestJSON is the release manifest content.
	ManifestJSON string
}

// DeployResult summarizes a deploy adapter result.
type DeployResult struct {
	// Targets are the nodes that were attempted.
	Targets []string
}

// RuntimeConfigInput provides desired-state config for live runtime queries.
type RuntimeConfigInput struct {
	// RepositoryPath is the repository subdirectory containing cluster.yml.
	RepositoryPath string
	// Cluster is the synced cluster.yml snapshot.
	Cluster configrepo.ClusterSnapshot
	// Apps are synced app config snapshots.
	Apps []configrepo.AppSnapshot
}

// ConfigNode describes a node from cluster.yml.
type ConfigNode struct {
	// ID is the node identifier.
	ID string
	// Host is the node SSH host.
	Host string
	// PrivateIP is the node private network address.
	PrivateIP string
}

// RuntimeStatus describes live cluster state from target nodes.
type RuntimeStatus struct {
	// Nodes are live node reachability records.
	Nodes []RuntimeNode
	// Apps are live app status records by node.
	Apps []RuntimeAppStatus
}

// RuntimeNode describes one cluster node in the live status view.
type RuntimeNode struct {
	// ID is the node identifier.
	ID string
	// Host is the node SSH host.
	Host string
	// PrivateIP is the node private network address.
	PrivateIP string
	// Reachable reports whether Warpgate reached the node.
	Reachable bool
}

// RuntimeAppStatus describes one app on one node.
type RuntimeAppStatus struct {
	// App is the application name.
	App string
	// NodeID is the node identifier.
	NodeID string
	// Version is the currently deployed version.
	Version string
	// Slot is the active deployment slot.
	Slot string
	// State is the live app state.
	State string
	// Services are live service status rows.
	Services []RuntimeContainerStatus
	// Error is set when live status could not be read.
	Error string
	// ShadowVersion is the current shadow version.
	ShadowVersion string
	// ShadowState is the current shadow state.
	ShadowState string
}

// RuntimeNodeStatus describes one app's status on one node.
type RuntimeNodeStatus struct {
	// NodeID is the node identifier.
	NodeID string
	// State is the live app state.
	State string
	// Version is the currently deployed version.
	Version string
	// Slot is the active deployment slot.
	Slot string
	// Containers is the docker compose status output.
	Containers string
	// Error is set when live status could not be read.
	Error string
	// ShadowVersion is the current shadow version.
	ShadowVersion string
	// ShadowState is the current shadow state.
	ShadowState string
}

// RuntimeContainerStatus describes a live compose service container.
type RuntimeContainerStatus struct {
	// Service is the compose service name.
	Service string
	// Name is the container name.
	Name string
	// State is the live container state.
	State string
}

// LogsInput describes a live logs request.
type LogsInput struct {
	// NodeID is the target node identifier.
	NodeID string
	// App filters logs to matching containers.
	App string
	// Tail is the number of recent lines.
	Tail int
	// Grep filters log lines server-side.
	Grep string
}

// LogsResult describes fetched live logs.
type LogsResult struct {
	// Output is raw prefixed log output.
	Output string
	// Message describes an empty result.
	Message string
}
