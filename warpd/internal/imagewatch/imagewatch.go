// Package imagewatch defines registry polling state.
package imagewatch

import "time"

const (
	// StatusReady means the image cursor has no pending digest change.
	StatusReady Status = "ready"
	// StatusChanged means a mutable tag resolved to a new digest.
	StatusChanged Status = "changed"
	// StatusUpdateAvailable means a newer tag matches the service's semver constraint.
	StatusUpdateAvailable Status = "update-available"
	// StatusInvalid means the image reference could not be checked.
	StatusInvalid Status = "invalid"
)

// Status is the image watch state shown in the UI.
type Status string

// Cursor records digest observations for one release service image.
type Cursor struct {
	// App is the app name.
	App string
	// Service is the release service name.
	Service string
	// Image is the registry image without tag or digest.
	Image string
	// Tag is the tag being watched: the pinned tag for semver-tracked
	// services, otherwise the mutable tag whose digest is observed.
	Tag string
	// Constraint is the semver constraint when the service tracks releases.
	Constraint string
	// CandidateTag is the highest registry tag matching Constraint.
	CandidateTag string
	// LastDigest is the most recent digest observed for the tag.
	LastDigest string
	// PreviousDigest is the prior digest when StatusChanged is set.
	PreviousDigest string
	// Status is the image watch state.
	Status Status
	// LastCheckedAt is when the registry was last checked.
	LastCheckedAt time.Time
	// LastError is the last registry error, if any.
	LastError string
}
