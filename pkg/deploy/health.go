package deploy

import (
	"fmt"
	"strings"
	"time"
)

const (
	healthPollInterval = 2 * time.Second
	healthTimeout      = 120 * time.Second
)

// HealthStatus represents the result of a container health check poll.
type HealthStatus int

const (
	// HealthStarting means the container is still starting up.
	HealthStarting HealthStatus = iota
	// HealthHealthy means the container passed its health check.
	HealthHealthy
	// HealthUnhealthy means the container failed its health check.
	HealthUnhealthy
	// HealthNone means the container has no health check configured.
	HealthNone
	// HealthNotFound means the container was not found.
	HealthNotFound
)

// WaitForHealthy polls a container's health status until it becomes healthy,
// unhealthy, or the timeout is reached.
func WaitForHealthy(runner commandRunner, composeDir, serviceName, projectFlag, composeFiles string, log logger) error {
	deadline := time.Now().Add(healthTimeout)

	inspectCmd := fmt.Sprintf(
		"cd %s && docker compose %s %s ps -q %s 2>/dev/null",
		composeDir, projectFlag, composeFiles, serviceName,
	)
	containerID, _, err := runner.RunCommand(inspectCmd)
	if err != nil || strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("container for %s not found after deploy", serviceName)
	}
	containerID = strings.TrimSpace(strings.Split(containerID, "\n")[0])

	hasHealthcheck, err := containerHasHealthcheck(runner, containerID)
	if err != nil {
		return fmt.Errorf("failed to check healthcheck config: %w", err)
	}
	if !hasHealthcheck {
		log.Infof("No healthcheck configured for %s, skipping health wait", serviceName)
		return nil
	}

	log.Infof("Waiting for %s to become healthy...", serviceName)

	for time.Now().Before(deadline) {
		status, err := pollHealth(runner, containerID)
		if err != nil {
			return fmt.Errorf("health poll failed: %w", err)
		}

		switch status {
		case HealthHealthy:
			log.Infof("Container %s is healthy", serviceName)
			return nil
		case HealthUnhealthy:
			logs := getHealthLogs(runner, containerID)
			return fmt.Errorf("container %s is unhealthy\n%s", serviceName, logs)
		case HealthNotFound:
			return fmt.Errorf("container %s disappeared during health check", serviceName)
		case HealthStarting:
			time.Sleep(healthPollInterval)
		case HealthNone:
			return nil
		}
	}

	return fmt.Errorf("health check timed out after %s for %s", healthTimeout, serviceName)
}

func containerHasHealthcheck(runner commandRunner, containerID string) (bool, error) {
	cmd := fmt.Sprintf("docker inspect --format '{{if .Config.Healthcheck}}true{{else}}false{{end}}' %s", containerID)
	stdout, _, err := runner.RunCommand(cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) == "true", nil
}

func pollHealth(runner commandRunner, containerID string) (HealthStatus, error) {
	cmd := fmt.Sprintf("docker inspect --format '{{.State.Health.Status}}' %s 2>/dev/null || echo 'not_found'", containerID)
	stdout, _, err := runner.RunCommand(cmd)
	if err != nil {
		return HealthNotFound, err
	}

	switch strings.TrimSpace(stdout) {
	case "healthy":
		return HealthHealthy, nil
	case "unhealthy":
		return HealthUnhealthy, nil
	case "starting":
		return HealthStarting, nil
	case "not_found":
		return HealthNotFound, nil
	default:
		return HealthNone, nil
	}
}

func getHealthLogs(runner commandRunner, containerID string) string {
	cmd := fmt.Sprintf("docker inspect --format '{{range .State.Health.Log}}{{.Output}}{{end}}' %s 2>/dev/null", containerID)
	stdout, _, _ := runner.RunCommand(cmd)
	output := strings.TrimSpace(stdout)
	if output == "" {
		return "(no health check output)"
	}
	return output
}

type logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}
