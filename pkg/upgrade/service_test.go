package upgrade

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUnitExistsFromLoadState(t *testing.T) {
	tests := []struct {
		name      string
		loadState string
		want      bool
	}{
		{name: "loaded unit exists", loadState: "loaded", want: true},
		{name: "not-found unit is missing", loadState: "not-found", want: false},
		{name: "empty load state is missing", loadState: "", want: false},
		{name: "whitespace loaded unit exists", loadState: "  loaded  ", want: true},
		{name: "whitespace not-found is missing", loadState: "  not-found  ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unitExistsFromLoadState(tt.loadState); got != tt.want {
				t.Fatalf("unitExistsFromLoadState(%q) = %v, want %v", tt.loadState, got, tt.want)
			}
		})
	}
}

func TestIsUnitActive(t *testing.T) {
	tests := []struct {
		name        string
		activeState string
		want        bool
	}{
		{name: "active is running", activeState: "active", want: true},
		{name: "whitespace active is running", activeState: "  active  ", want: true},
		{name: "inactive after stop is not running", activeState: "inactive", want: false},
		{name: "failed is not running", activeState: "failed", want: false},
		{name: "activating is not yet running", activeState: "activating", want: false},
		{name: "deactivating is not running", activeState: "deactivating", want: false},
		{name: "reloading is not running", activeState: "reloading", want: false},
		{name: "empty is not running", activeState: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnitActive(tt.activeState); got != tt.want {
				t.Fatalf("isUnitActive(%q) = %v, want %v", tt.activeState, got, tt.want)
			}
		})
	}
}

func TestStartWaitOutcome(t *testing.T) {
	tests := []struct {
		name        string
		activeState string
		want        startWaitResult
	}{
		{name: "active means start succeeded", activeState: "active", want: startWaitActive},
		{name: "trimmed active means start succeeded", activeState: "  active\n", want: startWaitActive},
		{name: "failed is terminal start failure", activeState: "failed", want: startWaitFailed},
		{name: "inactive keeps waiting", activeState: "inactive", want: startWaitContinue},
		{name: "activating keeps waiting", activeState: "activating", want: startWaitContinue},
		{name: "deactivating keeps waiting", activeState: "deactivating", want: startWaitContinue},
		{name: "reloading keeps waiting", activeState: "reloading", want: startWaitContinue},
		{name: "empty keeps waiting", activeState: "", want: startWaitContinue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := startWaitOutcome(tt.activeState); got != tt.want {
				t.Fatalf("startWaitOutcome(%q) = %v, want %v", tt.activeState, got, tt.want)
			}
		})
	}
}

func TestSystemdServiceManagerStateReportsInactiveWithoutError(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "loaded\n"},
		{kind: "output", args: []string{"show", "-p", "ActiveState", "--value", "warpgate.service"}, output: "inactive\n"},
	}}
	mgr := SystemdServiceManager{ctl: ctl}

	exists, active, err := mgr.State("warpgate")
	if err != nil {
		t.Fatalf("State() error = %v, want nil for inactive unit", err)
	}
	if !exists {
		t.Fatal("State() exists = false, want true")
	}
	if active {
		t.Fatal("State() active = true, want false")
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerStateMissingUnit(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "not-found\n"},
	}}
	mgr := SystemdServiceManager{ctl: ctl}

	exists, active, err := mgr.State("warpgate")
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if exists || active {
		t.Fatalf("State() = (%v, %v), want missing unit", exists, active)
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerStateEmptyName(t *testing.T) {
	ctl := &scriptedSystemctl{t: t}
	mgr := SystemdServiceManager{ctl: ctl}

	exists, active, err := mgr.State("  ")
	if err != nil || exists || active {
		t.Fatalf("State() = (%v, %v, %v), want (false, false, nil)", exists, active, err)
	}
	if len(ctl.calls) != 0 {
		t.Fatalf("State() issued systemctl calls %v, want none", ctl.calls)
	}
}

func TestSystemdServiceManagerStartAfterInactiveRunsStart(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "loaded\n"},
		{kind: "run", args: []string{"start", "warpgate.service"}},
		{kind: "output", args: []string{"show", "-p", "ActiveState", "--value", "warpgate.service"}, output: "inactive\n"},
		{kind: "output", args: []string{"show", "-p", "ActiveState", "--value", "warpgate.service"}, output: "active\n"},
	}}
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	mgr := SystemdServiceManager{ctl: ctl, now: clock.Now, sleep: clock.Sleep}

	if err := mgr.Start("warpgate"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !ctl.sawRun("start", "warpgate.service") {
		t.Fatal("Start() did not run systemctl start for inactive unit")
	}
	if clock.sleeps == 0 {
		t.Fatal("Start() did not poll ActiveState after start")
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerStartFailedUnit(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "loaded\n"},
		{kind: "run", args: []string{"start", "warpgate.service"}},
		{kind: "output", args: []string{"show", "-p", "ActiveState", "--value", "warpgate.service"}, output: "failed\n"},
	}}
	mgr := SystemdServiceManager{ctl: ctl, now: time.Now, sleep: func(time.Duration) {}}

	err := mgr.Start("warpgate")
	if err == nil {
		t.Fatal("Start() error = nil, want failed unit error")
	}
	if !strings.Contains(err.Error(), "ActiveState=failed") {
		t.Fatalf("Start() error = %v, want ActiveState=failed", err)
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerWaitUntilActiveTimesOut(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "ActiveState", "--value", "warpgate.service"}, output: "inactive\n"},
		{kind: "output", args: []string{"show", "-p", "ActiveState", "--value", "warpgate.service"}, output: "inactive\n"},
	}}
	clock := &fakeClock{now: time.Unix(0, 0)}
	mgr := SystemdServiceManager{ctl: ctl, now: clock.Now, sleep: clock.Sleep}

	err := mgr.waitUntilActive("warpgate.service", time.Second, time.Second)
	if err == nil {
		t.Fatal("waitUntilActive() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "did not become active") {
		t.Fatalf("waitUntilActive() error = %v, want did not become active", err)
	}
	if !strings.Contains(err.Error(), "ActiveState=inactive") {
		t.Fatalf("waitUntilActive() error = %v, want ActiveState=inactive", err)
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerStartMissingUnitIsNoop(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "not-found\n"},
	}}
	mgr := SystemdServiceManager{ctl: ctl}

	if err := mgr.Start("warpgate"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if ctl.sawRun("start", "warpgate.service") {
		t.Fatal("Start() ran systemctl start for missing unit")
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerStopExistingUnit(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "loaded\n"},
		{kind: "run", args: []string{"stop", "warpgate.service"}},
	}}
	mgr := SystemdServiceManager{ctl: ctl}

	if err := mgr.Stop("warpgate"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !ctl.sawRun("stop", "warpgate.service") {
		t.Fatal("Stop() did not run systemctl stop")
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerStopMissingUnitIsNoop(t *testing.T) {
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "not-found\n"},
	}}
	mgr := SystemdServiceManager{ctl: ctl}

	if err := mgr.Stop("warpgate"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if ctl.sawRun("stop", "warpgate.service") {
		t.Fatal("Stop() ran systemctl stop for missing unit")
	}
	ctl.assertExhausted()
}

func TestSystemdServiceManagerStartPropagatesSystemctlError(t *testing.T) {
	startErr := errors.New("permission denied")
	ctl := &scriptedSystemctl{t: t, steps: []systemctlStep{
		{kind: "output", args: []string{"show", "-p", "LoadState", "--value", "warpgate.service"}, output: "loaded\n"},
		{kind: "run", args: []string{"start", "warpgate.service"}, err: startErr},
	}}
	mgr := SystemdServiceManager{ctl: ctl}

	err := mgr.Start("warpgate")
	if !errors.Is(err, startErr) {
		t.Fatalf("Start() error = %v, want %v", err, startErr)
	}
	ctl.assertExhausted()
}

type systemctlStep struct {
	kind   string
	args   []string
	output string
	err    error
}

type scriptedSystemctl struct {
	t     *testing.T
	steps []systemctlStep
	i     int
	calls [][]string
}

func (s *scriptedSystemctl) Run(args ...string) error {
	s.calls = append(s.calls, append([]string{"run"}, args...))
	step := s.next("run", args)
	return step.err
}

func (s *scriptedSystemctl) Output(args ...string) (string, error) {
	s.calls = append(s.calls, append([]string{"output"}, args...))
	step := s.next("output", args)
	return step.output, step.err
}

func (s *scriptedSystemctl) next(kind string, args []string) systemctlStep {
	s.t.Helper()
	if s.i >= len(s.steps) {
		s.t.Fatalf("unexpected systemctl %s %v; no scripted steps left", kind, args)
	}
	step := s.steps[s.i]
	s.i++
	if step.kind != kind {
		s.t.Fatalf("systemctl call kind = %s, want %s (args %v)", kind, step.kind, args)
	}
	if !sameArgs(step.args, args) {
		s.t.Fatalf("systemctl args = %v, want %v", args, step.args)
	}
	return step
}

func (s *scriptedSystemctl) sawRun(args ...string) bool {
	for _, call := range s.calls {
		if len(call) == 0 || call[0] != "run" {
			continue
		}
		if sameArgs(call[1:], args) {
			return true
		}
	}
	return false
}

func (s *scriptedSystemctl) assertExhausted() {
	s.t.Helper()
	if s.i != len(s.steps) {
		s.t.Fatalf("unused systemctl steps: %d of %d consumed", s.i, len(s.steps))
	}
}

func sameArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type fakeClock struct {
	now    time.Time
	sleeps int
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps++
	c.now = c.now.Add(d)
}
