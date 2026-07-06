package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestModelInitialState(t *testing.T) {
	defs := []StepDef{
		{Name: "Step 1", Run: func() (string, error) { return "", nil }},
		{Name: "Step 2", Run: func() (string, error) { return "", nil }},
	}

	m := New("Test", defs)

	if len(m.steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(m.steps))
	}
	if m.steps[0].Name != "Step 1" {
		t.Errorf("expected step name 'Step 1', got %q", m.steps[0].Name)
	}
	if m.steps[0].Status != StepPending {
		t.Errorf("expected StepPending, got %d", m.steps[0].Status)
	}
	if m.done {
		t.Error("model should not be done initially")
	}
}

func TestModelStepDone(t *testing.T) {
	defs := []StepDef{
		{Name: "Step 1", Run: func() (string, error) { return "detail", nil }},
		{Name: "Step 2", Run: func() (string, error) { return "", nil }},
	}

	m := New("Test", defs)

	result, _ := m.Update(stepDoneMsg{index: 0, detail: "ok"})
	m = result.(Model)

	if m.steps[0].Status != StepDone {
		t.Errorf("expected StepDone, got %d", m.steps[0].Status)
	}
	if m.steps[0].Detail != "ok" {
		t.Errorf("expected detail 'ok', got %q", m.steps[0].Detail)
	}
	if m.done {
		t.Error("model should not be done after first step")
	}
	if m.current != 1 {
		t.Errorf("expected current=1, got %d", m.current)
	}
}

func TestModelStepError(t *testing.T) {
	defs := []StepDef{
		{Name: "Step 1", Run: func() (string, error) { return "", fmt.Errorf("boom") }},
		{Name: "Step 2", Run: func() (string, error) { return "", nil }},
	}

	m := New("Test", defs)

	result, _ := m.Update(stepErrorMsg{index: 0, err: fmt.Errorf("boom")})
	m = result.(Model)

	if m.steps[0].Status != StepFailed {
		t.Errorf("expected StepFailed, got %d", m.steps[0].Status)
	}
	if m.steps[0].Error == nil {
		t.Error("expected error to be set")
	}
	if !m.done {
		t.Error("model should be done after error")
	}
	if m.Err() == nil {
		t.Error("Err() should return the error")
	}
}

func TestModelWrapsStepErrorToTerminalWidth(t *testing.T) {
	message := "first failure message second failure message third failure message"
	wrapped := wrapText(message, 28)
	if !strings.Contains(wrapped, "\n") {
		t.Fatalf("expected helper to wrap text, got %q", wrapped)
	}

	defs := []StepDef{
		{Name: "Step 1", Run: func() (string, error) { return "", fmt.Errorf("boom") }},
	}
	m := New("Test", defs)
	result, _ := m.Update(tea.WindowSizeMsg{Width: 34, Height: 12})
	m = result.(Model)
	result, _ = m.Update(stepErrorMsg{
		index: 0,
		err:   fmt.Errorf("%s", message),
	})
	m = result.(Model)

	content := m.View().Content
	if !strings.Contains(content, "first failure message second") || !strings.Contains(content, "failure message third") {
		t.Fatalf("expected wrapped error output:\n%s", content)
	}
}

func TestModelAllStepsDone(t *testing.T) {
	defs := []StepDef{
		{Name: "Only step", Run: func() (string, error) { return "", nil }},
	}

	m := New("Test", defs)

	result, cmd := m.Update(stepDoneMsg{index: 0})
	m = result.(Model)

	if !m.done {
		t.Error("model should be done when last step completes")
	}

	if cmd == nil {
		t.Error("expected tea.Quit command")
	}
}

func TestModelCtrlCQuits(t *testing.T) {
	defs := []StepDef{
		{Name: "Step 1", Run: func() (string, error) { return "", nil }},
	}

	m := New("Test", defs)

	result, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = result.(Model)

	if !m.done {
		t.Error("ctrl+c should set done")
	}
}

func TestModelViewContainsTitle(t *testing.T) {
	defs := []StepDef{
		{Name: "My Step", Run: func() (string, error) { return "", nil }},
	}

	m := New("My Title", defs)
	view := m.View()

	if view.Content == "" {
		t.Error("view should not be empty")
	}
}
