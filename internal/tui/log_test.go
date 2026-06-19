package tui

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	applog "github.com/rampa069/ft8copilot/internal/log"
)

func rec(level slog.Level, msg string) slog.Record {
	return slog.NewRecord(time.Unix(0, 0).UTC(), level, msg, 0)
}

func TestLogViewNilSinkInert(t *testing.T) {
	lv := newLogView(nil, 10)
	if lv.listen() != nil {
		t.Error("nil-sink view should have no listen command")
	}
	if got := lv.render(40, 3); strings.TrimSpace(plain(got)) != "" {
		t.Errorf("nil-sink render should be blank, got %q", plain(got))
	}
}

func TestLogViewSeedsFromSnapshot(t *testing.T) {
	s := applog.NewSink(10, slog.LevelInfo)
	_ = s.Handle(context.Background(), rec(slog.LevelInfo, "seeded"))
	lv := newLogView(s, 10)
	defer lv.close()
	if e, ok := lv.last(); !ok || e.Message != "seeded" {
		t.Errorf("expected seeded entry, got %+v ok=%v", e, ok)
	}
}

func TestLogViewAddCaps(t *testing.T) {
	lv := newLogView(nil, 3)
	for _, m := range []string{"a", "b", "c", "d"} {
		lv.add(applog.Entry{Message: m})
	}
	if len(lv.entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(lv.entries))
	}
	if e, _ := lv.last(); e.Message != "d" {
		t.Errorf("last = %q, want d", e.Message)
	}
}

func TestLogViewRenderDimensions(t *testing.T) {
	lv := newLogView(nil, 10)
	lv.add(applog.Entry{Time: time.Unix(0, 0).UTC(), Level: slog.LevelInfo, Message: "hi"})
	out := lv.render(50, 4)
	if h := lipgloss.Height(out); h != 4 {
		t.Errorf("render height = %d, want 4 (padded)", h)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 50 {
			t.Errorf("line %d width = %d, want 50", i, w)
		}
	}
}

func TestLogViewListenDelivers(t *testing.T) {
	s := applog.NewSink(10, slog.LevelInfo)
	lv := newLogView(s, 10)
	defer lv.close()
	_ = s.Handle(context.Background(), rec(slog.LevelWarn, "incoming"))

	cmd := lv.listen()
	if cmd == nil {
		t.Fatal("expected a listen command")
	}
	msg := cmd()
	lm, ok := msg.(logMsg)
	if !ok {
		t.Fatalf("expected logMsg, got %T", msg)
	}
	if applog.Entry(lm).Message != "incoming" {
		t.Errorf("got %q, want incoming", applog.Entry(lm).Message)
	}
}

// The model should subscribe on Init and stream entries via logMsg.
func TestModelStreamsLog(t *testing.T) {
	s := applog.NewSink(10, slog.LevelInfo)
	m := newModel(Deps{LogSink: s})
	if m.Init() == nil {
		t.Fatal("Init should return the log listen command")
	}
	updated, cmd := m.Update(logMsg(applog.Entry{Message: "live"}))
	if cmd == nil {
		t.Error("logMsg should re-issue the listen command")
	}
	if e, ok := updated.(model).log.last(); !ok || e.Message != "live" {
		t.Errorf("model did not stash the streamed entry, got %+v ok=%v", e, ok)
	}
}

var _ tea.Model = model{}
