package ssh

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pangobit/warpgate/warpd/internal/stackstate"
)

func TestNewUIStylesWarningForeground(t *testing.T) {
	light := newUIStyles(false)
	dark := newUIStyles(true)

	if !colorsEqual(light.Notice.GetForeground(), lipgloss.Color(colorWarningLight)) {
		t.Fatalf("light notice foreground = %v, want %s", light.Notice.GetForeground(), colorWarningLight)
	}
	if !colorsEqual(light.Confirm.GetForeground(), lipgloss.Color(colorWarningLight)) {
		t.Fatalf("light confirm foreground = %v, want %s", light.Confirm.GetForeground(), colorWarningLight)
	}
	if !colorsEqual(dark.Notice.GetForeground(), lipgloss.Color(colorWarningDark)) {
		t.Fatalf("dark notice foreground = %v, want %s", dark.Notice.GetForeground(), colorWarningDark)
	}
	if !colorsEqual(dark.Confirm.GetForeground(), lipgloss.Color(colorWarningDark)) {
		t.Fatalf("dark confirm foreground = %v, want %s", dark.Confirm.GetForeground(), colorWarningDark)
	}
}

func TestStyleAttemptStatusRunningUsesNoticeStyle(t *testing.T) {
	m := newTestModel()
	got := m.styleAttemptStatus(stackstate.StatusRunning)
	want := m.styles.Notice.Render(string(stackstate.StatusRunning))
	if got != want {
		t.Fatalf("styleAttemptStatus(running) = %q, want %q", got, want)
	}
}

func TestBackgroundColorMsgRebuildsStyles(t *testing.T) {
	m := newTestModel()
	msg := tea.BackgroundColorMsg{}
	msg.Color = color.White
	next, cmd := m.Update(msg)
	if cmd != nil {
		t.Fatalf("expected no command after background color update, got %#v", cmd)
	}
	updated := next.(model)
	if updated.hasDarkBackground {
		t.Fatal("expected light background palette")
	}
	if !colorsEqual(updated.styles.Notice.GetForeground(), lipgloss.Color(colorWarningLight)) {
		t.Fatalf("notice foreground = %v, want %s", updated.styles.Notice.GetForeground(), colorWarningLight)
	}
}

func colorsEqual(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
