package log

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func record(level slog.Level, msg string) slog.Record {
	return slog.NewRecord(time.Unix(0, 0).UTC(), level, msg, 0)
}

func TestSinkRingEviction(t *testing.T) {
	s := NewSink(3, slog.LevelInfo)
	for _, m := range []string{"a", "b", "c", "d", "e"} {
		_ = s.Handle(context.Background(), record(slog.LevelInfo, m))
	}
	snap := s.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snap))
	}
	got := []string{snap[0].Message, snap[1].Message, snap[2].Message}
	want := []string{"c", "d", "e"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q (oldest first)", i, got[i], want[i])
		}
	}
}

func TestSinkLevelFilter(t *testing.T) {
	s := NewSink(10, slog.LevelWarn)
	if s.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("INFO should be disabled at WARN threshold")
	}
	if !s.Enabled(context.Background(), slog.LevelError) {
		t.Error("ERROR should be enabled at WARN threshold")
	}
}

func TestSinkAttrsRendered(t *testing.T) {
	s := NewSink(10, slog.LevelInfo)
	r := record(slog.LevelInfo, "calling")
	r.AddAttrs(slog.String("call", "CO8LY"), slog.Int("snr", -7))
	_ = s.Handle(context.Background(), r)
	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("want 1 entry, got %d", len(snap))
	}
	if snap[0].Message != "calling call=CO8LY snr=-7" {
		t.Errorf("message = %q", snap[0].Message)
	}
}

func TestSinkSubscribeReceives(t *testing.T) {
	s := NewSink(10, slog.LevelInfo)
	ch, cancel := s.Subscribe()
	defer cancel()
	_ = s.Handle(context.Background(), record(slog.LevelInfo, "hello"))
	select {
	case e := <-ch:
		if e.Message != "hello" {
			t.Errorf("got %q, want hello", e.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive entry")
	}
}

func TestSinkSubscribeNonBlocking(t *testing.T) {
	s := NewSink(10, slog.LevelInfo)
	// Subscribe but never drain; the channel buffers 256, so pushing many more
	// than that must not block Handle (entries are dropped).
	_, cancel := s.Subscribe()
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			_ = s.Handle(context.Background(), record(slog.LevelInfo, "x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle blocked when subscriber channel was full")
	}
}

func TestSinkUnsubscribeCloses(t *testing.T) {
	s := NewSink(10, slog.LevelInfo)
	ch, cancel := s.Subscribe()
	cancel()
	if _, ok := <-ch; ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestSinkWithGroupAndAttrs(t *testing.T) {
	s := NewSink(10, slog.LevelInfo)
	h := s.WithGroup("net").WithAttrs([]slog.Attr{slog.String("peer", "1.2.3.4")})
	_ = h.Handle(context.Background(), record(slog.LevelInfo, "connect"))
	// The derived handler must write to the same shared ring.
	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("want 1 shared entry, got %d", len(snap))
	}
	if snap[0].Message != "connect net.peer=1.2.3.4" {
		t.Errorf("message = %q", snap[0].Message)
	}
}
