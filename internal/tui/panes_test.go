package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/selector"
	"github.com/rampa069/ft8copilot/internal/sequencer"
)

func ranked(call, country string, snr int32, chosen, eligible bool) selector.Ranked {
	return selector.Ranked{
		Candidate: selector.Candidate{Record: db.Record{Call: call, Country: country, SNR: snr}},
		Eligible:  eligible,
		Chosen:    chosen,
	}
}

func TestRenderCandidatesLayout(t *testing.T) {
	rows := []selector.Ranked{
		ranked("OK1KKI", "Czech Republic", -3, true, true),
		ranked("W5JDC", "United States", -8, false, false),
	}
	out := renderCandidates(rows, 50, 6)
	if h := lipgloss.Height(out); h != 6 {
		t.Errorf("height = %d, want 6 (padded)", h)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 50 {
			t.Errorf("line %d width = %d, want 50", i, w)
		}
	}
	p := plain(out)
	if !strings.Contains(p, "CALL") || !strings.Contains(p, "COUNTRY") {
		t.Errorf("missing header columns:\n%s", p)
	}
	if !strings.Contains(p, "▶") {
		t.Errorf("chosen marker missing:\n%s", p)
	}
	if !strings.Contains(p, "OK1KKI") || !strings.Contains(p, "W5JDC") {
		t.Errorf("missing candidate calls:\n%s", p)
	}
}

func TestRenderStatusRunningVsPaused(t *testing.T) {
	rows := []selector.Ranked{
		ranked("OK1KKI", "Czech Republic", -3, true, true),
		ranked("W5JDC", "United States", -8, false, false),
	}
	running := plain(renderStatus(sequencer.Status{Band: 20, Frequency: 14074000}, rows, "EA5IUE", 26, 8))
	if !strings.Contains(running, "RUNNING") {
		t.Errorf("expected RUNNING, got:\n%s", running)
	}
	if !strings.Contains(running, "EA5IUE") {
		t.Errorf("expected station call, got:\n%s", running)
	}
	if !strings.Contains(running, "2 (1 ok)") {
		t.Errorf("expected spot counts '2 (1 ok)', got:\n%s", running)
	}

	paused := plain(renderStatus(sequencer.Status{Band: 20, Paused: true}, rows, "EA5IUE", 26, 8))
	if !strings.Contains(paused, "PAUSED") {
		t.Errorf("expected PAUSED, got:\n%s", paused)
	}
}

func TestRenderStatusDimensions(t *testing.T) {
	out := renderStatus(sequencer.Status{}, nil, "", 26, 8)
	if h := lipgloss.Height(out); h != 8 {
		t.Errorf("height = %d, want 8", h)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 26 {
			t.Errorf("line %d width = %d, want 26", i, w)
		}
	}
}

func TestViewShowsPausedIndicator(t *testing.T) {
	m := newModel(Deps{MyCall: "EA5IUE", Version: "test"})
	m.status = sequencer.Status{Band: 20, Paused: true}
	m.width, m.height = 84, 26
	view := plain(m.View())
	if !strings.Contains(view, "PAUSED") {
		t.Errorf("composed view should show PAUSED, got:\n%s", view)
	}
}
