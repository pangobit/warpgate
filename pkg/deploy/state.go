package deploy

import (
	"encoding/json"
	"time"
)

// DeployState tracks the current and previous deployment for rollback support.
type DeployState struct {
	// App is the application name.
	App string `json:"app"`
	// CurrentVersion is the currently deployed image tag.
	CurrentVersion string `json:"current_version"`
	// PreviousVersion is the previously deployed image tag (for rollback).
	PreviousVersion string `json:"previous_version"`
	// DeployedAt is the timestamp of the last deployment.
	DeployedAt time.Time `json:"deployed_at"`
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
