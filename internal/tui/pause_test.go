package tui

import (
	"io"
	"log/slog"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rampamac/ft8copilot/internal/config"
	"github.com/rampamac/ft8copilot/internal/db"
	"github.com/rampamac/ft8copilot/internal/sequencer"
)

// newTestSeq builds a real sequencer bound to an ephemeral UDP port (so it has a
// logger and the pause methods work) without needing WSJT-X.
func newTestSeq(t *testing.T) *sequencer.Sequencer {
	t.Helper()
	cmds := make(chan db.Command, 1)
	seq, err := sequencer.New(
		config.FT8Ctrl{WSJTIP: "127.0.0.1", WSJTPort: 0, TXRetries: 5},
		nil, cmds, slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("sequencer.New: %v", err)
	}
	t.Cleanup(func() { seq.Close() })
	return seq
}

func TestF2TogglesPause(t *testing.T) {
	seq := newTestSeq(t)
	m := newModel(Deps{Seq: seq})

	f2 := tea.KeyMsg{Type: tea.KeyF2}
	if _, _ = m.Update(f2); !seq.Paused() {
		t.Fatal("F2 should pause the autopilot")
	}
	if _, _ = m.Update(f2); seq.Paused() {
		t.Fatal("second F2 should resume the autopilot")
	}
}

func TestF2NilSeqNoPanic(t *testing.T) {
	m := newModel(Deps{})
	// Must not panic when no sequencer is wired.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF2}); cmd != nil {
		t.Errorf("expected nil command for F2 with no sequencer, got %T", cmd())
	}
}
