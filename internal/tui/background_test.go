package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// This file guards FT8CoPilot-z13: every cell of the TUI (panels and modal
// dialogs) must be painted with a background colour, so no "black gaps" show
// through where the desktop/panel background should be.

var sgrRE = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// countUnpaintedCells walks a rendered line, tracking whether a background
// colour is active (set by an SGR 4x/48 code, cleared by reset/49), and counts
// visible cells rendered with the terminal-default background.
func countUnpaintedCells(line string) int {
	i, n := 0, 0
	bg := false
	for i < len(line) {
		if loc := sgrRE.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
			for _, p := range strings.Split(sgrRE.FindStringSubmatch(line[i:])[1], ";") {
				switch {
				case p == "0" || p == "" || p == "49":
					bg = false
				case strings.HasPrefix(p, "4") && p != "49":
					bg = true
				}
			}
			i += loc[1]
			continue
		}
		sz := 1
		for _, r := range line[i:] {
			sz = len(string(r))
			break
		}
		if !bg {
			n++
		}
		i += sz
	}
	return n
}

func unpaintedTotal(view string) int {
	total := 0
	for _, ln := range strings.Split(view, "\n") {
		total += countUnpaintedCells(ln)
	}
	return total
}

func TestNoUnpaintedCells(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	newView := func(open tea.KeyType) string {
		m := newModel(Deps{MyCall: "EA5IUE", Version: "test"})
		m.width, m.height = 70, 20
		if open == 0 {
			m.refresh()
			return m.View()
		}
		u, _ := m.Update(tea.KeyMsg{Type: open})
		return u.(model).View()
	}

	// Panels and dialogs must be fully painted.
	for _, tc := range []struct {
		name string
		key  tea.KeyType
	}{
		{"main", 0},
		{"help", tea.KeyF1},
		{"candidates", tea.KeyF5},
		{"params", tea.KeyF4},
	} {
		if got := unpaintedTotal(newView(tc.key)); got != 0 {
			t.Errorf("%s view has %d unpainted (black) cells, want 0", tc.name, got)
		}
	}

	// The search view may show a single reverse-video cursor cell on the empty
	// input; everything else must be painted.
	if got := unpaintedTotal(newView(tea.KeyF3)); got > 1 {
		t.Errorf("search view has %d unpainted cells, want <= 1 (cursor)", got)
	}
}
