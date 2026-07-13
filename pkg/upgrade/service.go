package upgrade

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	serviceStartTimeout      = 30 * time.Second
	serviceStartPollInterval = 200 * time.Millisecond
)

// systemctlClient runs systemctl for unit control.
type systemctlClient interface {
	Run(args ...string) error
	Output(args ...string) (string, error)
}

// SystemdServiceManager controls a systemd unit during upgrade.
type SystemdServiceManager struct {
	ctl   systemctlClient
	now   func() time.Time
	sleep func(time.Duration)
}

// State reports whether a systemd unit exists and is active.
func (m SystemdServiceManager) State(serviceName string) (exists bool, active bool, err error) {
	if strings.TrimSpace(serviceName) == "" {
		return false, false, nil
	}

	unit := serviceName + ".service"
	exists, err = m.unitExists(unit)
	if err != nil || !exists {
		return exists, false, err
	}

	activeState, err := m.unitActiveState(unit)
	if err != nil {
		return true, false, err
	}
	return true, isUnitActive(activeState), nil
}

// Stop stops a systemd unit when it exists.
func (m SystemdServiceManager) Stop(serviceName string) error {
	if strings.TrimSpace(serviceName) == "" {
		return nil
	}
	unit := serviceName + ".service"
	exists, err := m.unitExists(unit)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return m.client().Run("stop", unit)
}

// Start starts a systemd unit when it exists and waits until it is active.
func (m SystemdServiceManager) Start(serviceName string) error {
	if strings.TrimSpace(serviceName) == "" {
		return nil
	}
	unit := serviceName + ".service"
	exists, err := m.unitExists(unit)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := m.client().Run("start", unit); err != nil {
		return err
	}
	return m.waitUntilActive(unit, serviceStartTimeout, serviceStartPollInterval)
}

func (m SystemdServiceManager) client() systemctlClient {
	if m.ctl != nil {
		return m.ctl
	}
	return execSystemctl{}
}

func (m SystemdServiceManager) unitExists(unit string) (bool, error) {
	loadState, err := m.client().Output("show", "-p", "LoadState", "--value", unit)
	if err != nil {
		return false, err
	}
	return unitExistsFromLoadState(loadState), nil
}

func (m SystemdServiceManager) unitActiveState(unit string) (string, error) {
	activeState, err := m.client().Output("show", "-p", "ActiveState", "--value", unit)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(activeState), nil
}

func (m SystemdServiceManager) waitUntilActive(unit string, timeout, interval time.Duration) error {
	now := m.now
	if now == nil {
		now = time.Now
	}
	sleep := m.sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	deadline := now().Add(timeout)
	var lastState string
	for {
		activeState, err := m.unitActiveState(unit)
		if err != nil {
			return err
		}
		lastState = activeState
		switch startWaitOutcome(activeState) {
		case startWaitActive:
			return nil
		case startWaitFailed:
			return fmt.Errorf("unit %s failed to start (ActiveState=%s)", unit, activeState)
		}
		if !now().Before(deadline) {
			return fmt.Errorf("unit %s did not become active (ActiveState=%s)", unit, lastState)
		}
		sleep(interval)
	}
}

type execSystemctl struct{}

func (execSystemctl) Run(args ...string) error {
	return runSystemctl(args...)
}

func (execSystemctl) Output(args ...string) (string, error) {
	return runSystemctlOutput(args...)
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

func unitExistsFromLoadState(loadState string) bool {
	loadState = strings.TrimSpace(loadState)
	return loadState != "" && loadState != "not-found"
}

func isUnitActive(activeState string) bool {
	return strings.TrimSpace(activeState) == "active"
}

type startWaitResult int

const (
	startWaitContinue startWaitResult = iota
	startWaitActive
	startWaitFailed
)

func startWaitOutcome(activeState string) startWaitResult {
	switch strings.TrimSpace(activeState) {
	case "active":
		return startWaitActive
	case "failed":
		return startWaitFailed
	default:
		return startWaitContinue
	}
}
