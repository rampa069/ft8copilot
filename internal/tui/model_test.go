package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ansiRE strips SGR/escape sequences so tests can assert on visible text.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestViewEmptyBeforeSize(t *testing.T) {
	m := newModel(Deps{MyCall: "CO8LY", Version: "test"})
	if got := m.View(); got != "" {
		t.Fatalf("expected empty view before WindowSizeMsg, got %q", got)
	}
}

func TestViewRendersAfterSize(t *testing.T) {
	m := newModel(Deps{MyCall: "CO8LY", Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := plain(updated.View())
	if view == "" {
		t.Fatal("expected non-empty view after WindowSizeMsg")
	}
	if !strings.Contains(view, "CO8LY") {
		t.Errorf("expected header to contain the callsign, got:\n%s", view)
	}
	if !strings.Contains(view, "F10 Quit") {
		t.Errorf("expected footer to contain the F10 Quit key, got:\n%s", view)
	}
}

func TestQuitKeys(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyF10},
	}
	for _, k := range keys {
		m := newModel(Deps{})
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("key %q: expected a command, got nil", k.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q: expected tea.QuitMsg, got %T", k.String(), cmd())
		}
	}
}
