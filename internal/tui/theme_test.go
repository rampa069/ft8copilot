package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBoxDimensions(t *testing.T) {
	out := box("Title", "line one\nline two", 30, 8, false)
	if h := lipgloss.Height(out); h != 8 {
		t.Errorf("box height = %d, want 8", h)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 30 {
			t.Errorf("line %d width = %d, want 30", i, w)
		}
	}
	if !strings.Contains(plain(out), "Title") {
		t.Errorf("box should embed the title, got:\n%s", plain(out))
	}
}

func TestBoxTitleTooWideFallsBack(t *testing.T) {
	// A title wider than the interior must not panic or overflow the width.
	out := box("A very long title that will not fit", "body", 12, 4, true)
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 12 {
			t.Errorf("line %d width = %d, want 12", i, w)
		}
	}
}

func TestFit(t *testing.T) {
	if got := plain(fit("abc", 5)); got != "abc  " {
		t.Errorf("fit pad = %q, want %q", got, "abc  ")
	}
	if got := fit("abcdef", 3); got != "abc" {
		t.Errorf("fit clip = %q, want %q", got, "abc")
	}
	if got := fit("", 0); got != "" {
		t.Errorf("fit zero width = %q, want empty", got)
	}
}

func TestFunctionBarWidth(t *testing.T) {
	bar := functionBar(60, fkeys)
	if w := lipgloss.Width(bar); w != 60 {
		t.Errorf("function bar width = %d, want 60", w)
	}
	if !strings.Contains(plain(bar), "F2 Pause") {
		t.Errorf("function bar should list F2 Pause, got:\n%s", plain(bar))
	}
}
