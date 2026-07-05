package upgrade

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// SystemdServiceManager controls a systemd unit during upgrade.
type SystemdServiceManager struct{}

// State reports whether a systemd unit exists and is active.
func (SystemdServiceManager) State(serviceName string) (exists bool, active bool, err error) {
	if strings.TrimSpace(serviceName) == "" {
		return false, false, nil
	}

	unit := serviceName + ".service"
	if err := runSystemctl("status", unit); err != nil {
		if isMissingUnit(err) {
			return false, false, nil
		}
		return false, false, err
	}

	output, err := runSystemctlOutput("is-active", unit)
	if err != nil {
		if isInactiveUnit(err) {
			return true, false, nil
		}
		return true, false, err
	}
	return true, strings.TrimSpace(output) == "active", nil
}

// Stop stops a systemd unit when it exists.
func (SystemdServiceManager) Stop(serviceName string) error {
	exists, _, err := SystemdServiceManager{}.State(serviceName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return runSystemctl("stop", serviceName+".service")
}

// Start starts a systemd unit when it exists.
func (SystemdServiceManager) Start(serviceName string) error {
	exists, _, err := SystemdServiceManager{}.State(serviceName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return runSystemctl("start", serviceName+".service")
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), message)
	}
	return nil
}

func runSystemctlOutput(args ...string) (string, error) {
	cmd := exec.Command("systemctl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), message)
	}
	return stdout.String(), nil
}

func isMissingUnit(err error) bool {
	message := err.Error()
	return strings.Contains(message, "could not be found") ||
		strings.Contains(message, "Unit") && strings.Contains(message, "not found") ||
		strings.Contains(message, "Load state not-found")
}

func isInactiveUnit(err error) bool {
	message := err.Error()
	return strings.Contains(message, "inactive") ||
		strings.Contains(message, "failed") ||
		strings.Contains(message, "deactivating")
}
