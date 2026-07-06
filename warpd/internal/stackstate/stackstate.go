// Package stackstate tracks whole-stack deploy attempts and the last healthy baseline.
package stackstate

import "time"

const (
	// StatusRunning means the stack deploy is in progress.
	StatusRunning Status = "running"
	// StatusSucceeded means every app deployed healthy and the baseline advanced.
	StatusSucceeded Status = "succeeded"
	// StatusFailed means the deploy failed with no healthy baseline to revert to.
	StatusFailed Status = "failed"
	// StatusReverted means the deploy failed and the stack returned to the last healthy baseline.
	StatusReverted Status = "reverted"
	// StatusRevertFailed means the deploy failed and reverting also failed; the stack needs operator attention.
	StatusRevertFailed Status = "revert-failed"
)

// Status is the stack deploy attempt state.
type Status string

// Snapshot records the release set that was last healthy as a whole.
type Snapshot struct {
	// Releases maps app names to deployed release IDs.
	Releases map[string]string
	// ClusterFileSHA is the cluster.yml blob SHA from the last healthy deploy.
	ClusterFileSHA string
	// AppConfigSHAs maps app names to app.yml blob SHAs from the last healthy deploy.
	AppConfigSHAs map[string]string
	// UpdatedAt is when the baseline advanced.
	UpdatedAt time.Time
}

// Attempt records one whole-stack deploy attempt.
type Attempt struct {
	// ID is the attempt identifier.
	ID string
	// Status is the attempt state.
	Status Status
	// Releases maps app names to the release IDs this attempt deployed.
	Releases map[string]string
	// ActorEmail is the operator who started the attempt.
	ActorEmail string
	// StartedAt is when the attempt started.
	StartedAt time.Time
	// FinishedAt is when the attempt finished, including any revert.
	FinishedAt *time.Time
	// FailedApp is the app whose deploy failed.
	FailedApp string
	// Error is the deploy failure message.
	Error string
	// RevertError is the revert failure message when reverting also failed.
	RevertError string
	// DeployedApps lists apps that ran DeployRelease during this attempt.
	DeployedApps []string
	// SkippedApps lists apps left unchanged during a selective deploy.
	SkippedApps []string
	// Forced reports whether the operator forced a full stack redeploy.
	Forced bool
}

// State is the persisted whole-stack deploy state.
type State struct {
	// LastHealthy is the baseline the stack reverts to on failure.
	LastHealthy Snapshot
	// LastAttempt is the most recent stack deploy attempt.
	LastAttempt *Attempt
}
