package sequencer

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/selector"
)

// stubSelector records how many times the chain was consulted and returns a
// fixed candidate, so the pause gate can be tested without a database.
type stubSelector struct {
	name   string
	called *int
	cand   selector.Candidate
	ok     bool
}

func (s stubSelector) Name() string { return s.name }

func (s stubSelector) Get(int) (selector.Candidate, bool) {
	*s.called++
	return s.cand, s.ok
}

func TestPauseGatesSequenceCheck(t *testing.T) {
	called := 0
	stub := stubSelector{
		name:   "Stub",
		called: &called,
		cand:   selector.Candidate{Record: db.Record{Call: "CO8LY"}},
		ok:     true,
	}
	s := &Sequencer{
		chain:      selector.Chain{stub},
		sequence:   map[int]bool{},
		lastSecond: -1,
		tracker:    txTracker{max: 5},
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		// peer stays nil so callStation is a no-op (no UDP needed).
	}

	fixed := time.Date(2026, 6, 19, 12, 0, 15, 0, time.UTC)
	restore := nowFunc
	nowFunc = func() time.Time { return fixed }
	defer func() { nowFunc = restore }()
	s.sequence[fixed.UTC().Second()] = true

	// Running: the chain is consulted and a station is selected.
	s.sequenceCheck()
	if called != 1 {
		t.Fatalf("running: chain consulted %d times, want 1", called)
	}
	if s.current != "CO8LY" {
		t.Errorf("running: current = %q, want CO8LY", s.current)
	}

	// Paused: the chain must not be consulted and no station selected.
	s.lastSecond = -1
	s.current = ""
	s.Pause()
	if !s.Paused() {
		t.Fatal("Paused() should report true after Pause()")
	}
	s.sequenceCheck()
	if called != 1 {
		t.Errorf("paused: chain consulted (called=%d), want still 1", called)
	}
	if s.current != "" {
		t.Errorf("paused: current = %q, want empty", s.current)
	}

	// Resumed: calling resumes.
	s.lastSecond = -1
	s.Resume()
	if s.Paused() {
		t.Fatal("Paused() should report false after Resume()")
	}
	s.sequenceCheck()
	if called != 2 {
		t.Errorf("resumed: chain consulted %d times, want 2", called)
	}
}

func TestTogglePause(t *testing.T) {
	s := &Sequencer{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if s.Paused() {
		t.Fatal("new sequencer should not be paused")
	}
	if got := s.TogglePause(); !got || !s.Paused() {
		t.Errorf("first toggle: got %v, Paused()=%v, want both true", got, s.Paused())
	}
	if got := s.TogglePause(); got || s.Paused() {
		t.Errorf("second toggle: got %v, Paused()=%v, want both false", got, s.Paused())
	}
}
