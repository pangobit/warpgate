package deploy

import (
	"encoding/json"
	"time"
)

// DeployState tracks the current and previous deployment for rollback and blue/green support.
type DeployState struct {
	// App is the application name.
	App string `json:"app"`
	// CurrentRelease is the currently deployed release ID.
	CurrentRelease string `json:"current_release,omitempty"`
	// PreviousRelease is the previously deployed release ID for rollback.
	PreviousRelease string `json:"previous_release,omitempty"`
	// CurrentReleaseInputs captures the tuple that produced the current release.
	CurrentReleaseInputs ReleaseInputs `json:"current_release_inputs,omitempty"`
	// PreviousReleaseInputs captures the tuple that produced the previous release.
	PreviousReleaseInputs ReleaseInputs `json:"previous_release_inputs,omitempty"`
	// CurrentVersion is the currently deployed image tag.
	CurrentVersion string `json:"current_version"`
	// PreviousVersion is the previously deployed image tag (for rollback).
	PreviousVersion string `json:"previous_version"`
	// ActiveSlot is the currently active blue/green slot ("blue" or "green").
	// Recreate deployments leave this empty.
	ActiveSlot string `json:"active_slot"`
	// DeployedAt is the timestamp of the last deployment.
	DeployedAt time.Time `json:"deployed_at"`
	// ShadowVersion is the image tag of the shadow deployment, if any.
	ShadowVersion string `json:"shadow_version,omitempty"`
	// ShadowDeployedAt is the timestamp of the shadow deployment.
	ShadowDeployedAt *time.Time `json:"shadow_deployed_at,omitempty"`
}

// ReleaseInputs identifies the release construction inputs recorded in deploy state.
type ReleaseInputs struct {
	// ImageRef is the image reference used by the release.
	ImageRef string `json:"image_ref,omitempty"`
	// ComposeRev identifies the compose shape used by the release.
	ComposeRev string `json:"compose_rev,omitempty"`
	// EnvHash identifies the environment layer used by the release.
	EnvHash string `json:"env_hash,omitempty"`
}

// InactiveSlot returns the slot that is not currently active.
func (s *DeployState) InactiveSlot() string {
	if s.ActiveSlot == "green" {
		return "blue"
	}
	return "green"
}

// Marshal serializes the deploy state to JSON.
func (s *DeployState) Marshal() (string, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// UnmarshalState deserializes deploy state from JSON.
func UnmarshalState(data string) (*DeployState, error) {
	var state DeployState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return nil, err
	}
	return &state, nil
}
