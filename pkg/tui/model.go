package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

const (
	defaultStepWidth = 100
	minStepWidth     = 20
)

// StepStatus represents the current state of a step.
type StepStatus int

const (
	// StepPending means the step has not started.
	StepPending StepStatus = iota
	// StepRunning means the step is currently executing.
	StepRunning
	// StepDone means the step completed successfully.
	StepDone
	// StepFailed means the step failed with an error.
	StepFailed
)

// StepState holds the name, status, and optional detail for a step.
type StepState struct {
	// Name is the display name of the step.
	Name string
	// Status is the current step status.
	Status StepStatus
	// Detail is optional text shown after the step name when done (e.g. OS info).
	Detail string
	// Error is set when the step fails.
	Error error
}

// StepFunc is a function that executes a step and returns an optional detail string.
// It is called in a goroutine by bubbletea.
type StepFunc func() (detail string, err error)

// StepDef defines a step to be run by the TUI.
type StepDef struct {
	// Name is the display name.
	Name string
	// Run executes the step.
	Run StepFunc
}

type stepDoneMsg struct {
	index  int
	detail string
}

type stepErrorMsg struct {
	index int
	err   error
}

// Model is the bubbletea model for the step-runner TUI.
type Model struct {
	title   string
	steps   []StepState
	defs    []StepDef
	current int
	spinner spinner.Model
	done    bool
	err     error
	width   int
}

// New creates a new step-runner model.
func New(title string, defs []StepDef) Model {
	steps := make([]StepState, len(defs))
	for i, d := range defs {
		steps[i] = StepState{Name: d.Name}
	}

	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))

	return Model{
		title:   title,
		steps:   steps,
		defs:    defs,
		spinner: s,
		width:   defaultStepWidth,
	}
}

// Init starts the spinner and kicks off the first step.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runStep(0))
}

// Update handles messages and advances steps.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case stepDoneMsg:
		if msg.index < len(m.steps) {
			m.steps[msg.index].Status = StepDone
			m.steps[msg.index].Detail = msg.detail
		}

		next := msg.index + 1
		if next >= len(m.steps) {
			m.done = true
			return m, tea.Quit
		}

		m.current = next
		return m, m.runStep(next)

	case stepErrorMsg:
		if msg.index < len(m.steps) {
			m.steps[msg.index].Status = StepFailed
			m.steps[msg.index].Error = msg.err
		}
		m.err = msg.err
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// View renders the step list.
func (m Model) View() tea.View {
	var b strings.Builder

	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")

	for i, step := range m.steps {
		switch step.Status {
		case StepDone:
			line := doneIcon + " " + stepStyle.Render(step.Name)
			if step.Detail != "" {
				line += " " + detailStyle.Render("("+step.Detail+")")
			}
			b.WriteString(line)
		case StepRunning:
			b.WriteString(m.spinner.View() + " " + runningStyle.Render(step.Name))
		case StepFailed:
			b.WriteString(failIcon + " " + stepStyle.Render(step.Name))
			if step.Error != nil {
				b.WriteString("\n" + errorStyle.Render(wrapText(step.Error.Error(), m.errorWidth())))
			}
		default:
			b.WriteString(pendingIcon + " " + pendingStyle.Render(step.Name))
		}

		if i < len(m.steps)-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	return tea.NewView(b.String())
}

// Err returns the error if a step failed.
func (m Model) Err() error {
	return m.err
}

func (m Model) errorWidth() int {
	if m.width <= 0 {
		return defaultStepWidth
	}
	return max(minStepWidth, m.width-6)
}

func wrapText(text string, width int) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= width {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	var lines []string
	var line strings.Builder
	for _, word := range words {
		if line.Len() == 0 {
			line.WriteString(word)
			continue
		}
		if len([]rune(line.String()))+1+len([]rune(word)) > width {
			lines = append(lines, line.String())
			line.Reset()
			line.WriteString(word)
			continue
		}
		line.WriteString(" ")
		line.WriteString(word)
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func (m Model) runStep(idx int) tea.Cmd {
	if idx >= len(m.defs) {
		return nil
	}

	m.steps[idx].Status = StepRunning
	def := m.defs[idx]

	return func() tea.Msg {
		detail, err := def.Run()
		if err != nil {
			return stepErrorMsg{index: idx, err: err}
		}
		return stepDoneMsg{index: idx, detail: detail}
	}
}

// Run creates a bubbletea program and runs the step-runner TUI to completion.
func Run(title string, defs []StepDef) error {
	m := New(title, defs)
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if final, ok := result.(Model); ok {
		return final.Err()
	}

	return nil
}
