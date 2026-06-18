package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"WARNING": slog.LevelWarn,
		"error":   slog.LevelError,
		"bogus":   slog.LevelInfo,
		" Debug ": slog.LevelDebug,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	// max=10 bytes, 2 backups, so a few writes force rotation.
	w, err := newRotatingWriter(path, 10, 2)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte("0123456789")); err != nil { // 10 bytes each
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The live file plus at most `backups` rotated files should exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("live log missing: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected rotated backup .1: %v", err)
	}
	// .3 must never exist (backups=2).
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Error("backup .3 exists, exceeds backups=2")
	}
}

func TestSetupConsoleOnly(t *testing.T) {
	logger, closer := Setup("")
	if logger == nil {
		t.Fatal("nil logger")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("closer: %v", err)
	}
}

func TestSetupWithFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ft8ctrl-debug.log")
	logger, closer := Setup(path)
	defer closer.Close()
	logger.Debug("hello", "k", "v")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("log file missing entry: %q", data)
	}
}
