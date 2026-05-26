// Package deployment defines daemon-owned deployment records.
package deployment

import "time"

const (
	// StatusQueued identifies a deployment waiting to run.
	StatusQueued Status = "queued"
	// StatusRunning identifies a deployment in progress.
	StatusRunning Status = "running"
	// StatusSucceeded identifies a successful deployment.
	StatusSucceeded Status = "succeeded"
	// StatusFailed identifies a failed deployment.
	StatusFailed Status = "failed"
)

// Status is the persisted deployment lifecycle state.
type Status string

// Record captures one attempt to deploy a release.
type Record struct {
	// ID is the deployment identifier.
	ID string
	// ReleaseID is the release being deployed.
	ReleaseID string
	// App is the app being deployed.
	App string
	// Targets are the requested target nodes.
	Targets []string
	// ActorEmail is the operator who started the deployment.
	ActorEmail string
	// Status is the deployment lifecycle state.
	Status Status
	// StartedAt is when execution began.
	StartedAt time.Time
	// FinishedAt is when execution finished.
	FinishedAt *time.Time
	// ErrorMessage summarizes a failed deployment.
	ErrorMessage string
}
