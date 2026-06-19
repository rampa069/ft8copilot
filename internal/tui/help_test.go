package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestHelpViewDimensions(t *testing.T) {
	out := helpView(84, 24)
	if w := lipgloss.Width(out); w != 84 {
		t.Errorf("help view width = %d, want 84", w)
	}
	p := plain(out)
	for _, want := range []string{"Help", "F2 / Space", "Pause / resume", "Quit"} {
		if !strings.Contains(p, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestF1OpensAndClosesHelp(t *testing.T) {
	m := newModel(Deps{MyCall: "EA5IUE", Version: "test"})
	m.width, m.height = 84, 26

	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyF1})
	mm := opened.(model)
	if !mm.help {
		t.Fatal("F1 should open help")
	}
	if !strings.Contains(plain(mm.View()), "This help screen") {
		t.Error("composed view should show the help dialog")
	}

	closed, _ := mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if closed.(model).help {
		t.Error("Esc should close help")
	}
}
