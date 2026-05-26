// Package release defines daemon-owned release records.
package release

import "time"

const (
	// StatusDraft identifies a release being edited.
	StatusDraft Status = "draft"
	// StatusReady identifies a release ready to deploy.
	StatusReady Status = "ready"
	// StatusDeploying identifies a release with an active deployment.
	StatusDeploying Status = "deploying"
	// StatusDeployed identifies a release whose latest deployment succeeded.
	StatusDeployed Status = "deployed"
	// StatusFailed identifies a release whose latest deployment failed.
	StatusFailed Status = "failed"
)

// Status is the persisted release lifecycle state.
type Status string

// Record is a durable release derived from committed desired state.
type Record struct {
	// ID is the release identifier.
	ID string
	// App is the app name.
	App string
	// ConfigCommit is the GitHub commit SHA that produced the release.
	ConfigCommit string
	// ManifestJSON is the immutable deploy manifest as JSON.
	ManifestJSON string
	// RawYAML is the committed app.yml content.
	RawYAML string
	// Status is the release lifecycle state.
	Status Status
	// ActorEmail is the operator who created the release.
	ActorEmail string
	// CreatedAt is when the release was created.
	CreatedAt time.Time
}

// DeployDataChange is a structured edit to release-owned deploy data.
type DeployDataChange struct {
	// Service is the release service name being changed.
	Service string
	// ImageTag is the new mutable image tag.
	ImageTag string
	// ImageDigest is the new immutable image digest.
	ImageDigest string
	// Environment replaces or adds non-secret environment values.
	Environment map[string]string
	// Targets replaces the app target node list when non-nil.
	Targets []string
	// Strategy replaces the app deploy strategy when non-empty.
	Strategy string
}

// CommitPreview describes the commit that will create a release.
type CommitPreview struct {
	// Path is the repository path to be written.
	Path string
	// Message is the Git commit message.
	Message string
	// YAML is the generated app.yml content.
	YAML string
}
