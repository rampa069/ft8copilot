package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/selector"
)

func rankedFull(call, country string, snr int32, chosen, eligible bool, reason string) selector.Ranked {
	return selector.Ranked{
		Candidate: selector.Candidate{Record: db.Record{Call: call, Country: country, SNR: snr}},
		Eligible:  eligible,
		Chosen:    chosen,
		Reason:    reason,
	}
}

func TestRenderCandidatesFullLayout(t *testing.T) {
	rows := []selector.Ranked{
		rankedFull("OK1KKI", "Czech Republic", -3, true, true, ""),
		rankedFull("W5JDC", "United States", -8, false, false, "blacklist"),
	}
	out := renderCandidatesFull(rows, 82, 6, 0)
	if h := lipgloss.Height(out); h != 6 {
		t.Errorf("height = %d, want 6", h)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 82 {
			t.Errorf("line %d width = %d, want 82", i, w)
		}
	}
	p := plain(out)
	for _, want := range []string{"CALL", "CQ", "ITU", "STATUS", "▶", "blacklist"} {
		if !strings.Contains(p, want) {
			t.Errorf("output missing %q:\n%s", want, p)
		}
	}
}

func TestRenderCandidatesFullScrollOffset(t *testing.T) {
	rows := make([]selector.Ranked, 20)
	for i := range rows {
		rows[i] = rankedFull("CALL", "Country", int32(-i), false, true, "")
	}
	// Offset beyond the end must clamp, not panic or blank out.
	out := renderCandidatesFull(rows, 82, 5, 999)
	if h := lipgloss.Height(out); h != 5 {
		t.Errorf("height = %d, want 5", h)
	}
}

func TestModelF5OpensCandidatesView(t *testing.T) {
	m := newModel(Deps{})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	mm := updated.(model)
	if !mm.candidates {
		t.Fatal("F5 should open the candidates view")
	}
	// down then up scrolls.
	mm, _ = upd(mm, tea.KeyMsg{Type: tea.KeyDown})
	if mm.candOffset != 1 {
		t.Errorf("offset after down = %d, want 1", mm.candOffset)
	}
	mm, _ = upd(mm, tea.KeyMsg{Type: tea.KeyUp})
	if mm.candOffset != 0 {
		t.Errorf("offset after up = %d, want 0", mm.candOffset)
	}
	// F5 again closes.
	mm, _ = upd(mm, tea.KeyMsg{Type: tea.KeyF5})
	if mm.candidates {
		t.Error("second F5 should close the candidates view")
	}
}

// upd is a small helper to thread the concrete model type through Update.
func upd(m model, msg tea.Msg) (model, tea.Cmd) {
	updated, cmd := m.Update(msg)
	return updated.(model), cmd
}
