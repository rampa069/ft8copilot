package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Guards the F4 Parameters editor against the "descuajeringa" bug: a field value
// longer than its column (the Selectors/Blacklist chains) used to overflow the
// panel interior, so box()'s ANSI-naive padding mangled the row and the panel
// went ragged as focus moved. Every rendered line must stay the panel width for
// every focused field.
func TestParamsLayoutStableAcrossFocus(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)

	m := newModel(Deps{MyCall: "EA5IUE", Version: "test"})
	m.width, m.height = 80, 26
	var cur tea.Model = m
	cur, _ = cur.Update(tea.KeyMsg{Type: tea.KeyF4})

	mm := cur.(model)
	// Values wider than the field column, the case that used to break.
	mm.params.inputs[fldSelectors].SetValue("Any CallSign Grid Continent Country DXCC100 Extra")
	mm.params.inputs[fldBlackList].SetValue("KC5TT, KD7DPS, VA7QI, W5JDC, W6IPA, AB1CD, EF2GH")

	for f := 0; f < numFields; f++ {
		mm.params.setFocus(f)
		view := mm.View()

		widths := map[int]bool{}
		for _, ln := range strings.Split(view, "\n") {
			widths[lipgloss.Width(sgr.ReplaceAllString(ln, ""))] = true
		}
		if len(widths) != 1 {
			t.Errorf("focus=%d: ragged layout, distinct line widths=%v (want all equal)", f, widths)
		}
		// The focused field shows a single reverse-video cursor cell (as the
		// search modal does); everything else must be painted.
		if got := unpaintedTotal(view); got > 1 {
			t.Errorf("focus=%d: %d unpainted cells, want <= 1 (cursor)", f, got)
		}
	}
}
