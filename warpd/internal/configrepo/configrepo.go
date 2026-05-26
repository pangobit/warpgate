// Package configrepo defines desired-state repository records.
package configrepo

import "time"

// RepositorySettings identifies an attached GitHub infrastructure repository.
type RepositorySettings struct {
	// Owner is the GitHub repository owner.
	Owner string
	// Repo is the GitHub repository name.
	Repo string
	// Branch is the branch Warpgate reads from and writes to.
	Branch string
	// TokenEnvVar is the environment variable that contains the GitHub token.
	TokenEnvVar string
	// AttachedAt is when the repository was attached to this daemon.
	AttachedAt time.Time
}

// AppSnapshot is the latest observed app config from the desired-state repo.
type AppSnapshot struct {
	// Name is derived from apps/<name>/.
	Name string
	// Path is the repository path to the app config.
	Path string
	// ConfigCommit is the commit SHA that produced RawYAML.
	ConfigCommit string
	// FileSHA is the GitHub blob SHA used for optimistic writes.
	FileSHA string
	// RawYAML is the app.yml content.
	RawYAML string
	// ComposeYAML is the optional compose.yml content.
	ComposeYAML string
	// UpdatedAt is when this snapshot was persisted.
	UpdatedAt time.Time
}

// SyncCursor records the last config sync attempt.
type SyncCursor struct {
	// LastObservedCommit is the latest branch head seen by Warpgate.
	LastObservedCommit string
	// LastCheckedAt is when the repository was last checked.
	LastCheckedAt time.Time
	// LastError is the last sync error, if any.
	LastError string
}

// PollerSettings controls daemon polling for a repository.
type PollerSettings struct {
	// ConfigEnabled enables scheduled config sync.
	ConfigEnabled bool
	// ConfigInterval is the config poll interval.
	ConfigInterval time.Duration
	// ImagesEnabled enables scheduled image sync.
	ImagesEnabled bool
	// ImagesInterval is the image poll interval.
	ImagesInterval time.Duration
}
