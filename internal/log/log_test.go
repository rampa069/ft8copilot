package log

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleHandlerPlain(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(newConsoleHandler(&buf, slog.LevelInfo, false))
	l.Info("calling", "call", "EA5IUE", "band", 20, "country", "Spain")
	out := buf.String()
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain output must not contain ANSI: %q", out)
	}
	for _, want := range []string{"INFO ", "calling", "call=EA5IUE", "band=20", "country=Spain"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestConsoleHandlerColor(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(newConsoleHandler(&buf, slog.LevelInfo, true))
	l.Warn("retries exceeded", "call", "EA5IUE")
	out := buf.String()
	if !strings.Contains(out, ansiYellow) {
		t.Errorf("WARN should be yellow: %q", out)
	}
	if !strings.Contains(out, ansiBoldCyan) {
		t.Errorf("call value should be highlighted: %q", out)
	}
	if !strings.Contains(out, ansiReset) {
		t.Errorf("colour codes must be reset: %q", out)
	}
}

func TestConsoleHandlerQuotesSpaces(t *testing.T) {
	var buf bytes.Buffer
	slog.New(newConsoleHandler(&buf, slog.LevelInfo, false)).
		Info("reload", "field", "wsjt_ip", "was", "Asiatic Russia")
	if !strings.Contains(buf.String(), `was="Asiatic Russia"`) {
		t.Errorf("value with spaces not quoted: %q", buf.String())
	}
}

func TestUseColor(t *testing.T) {
	t.Setenv("FT8_COLOR", "always")
	t.Setenv("NO_COLOR", "1")
	if !useColor(os.Stderr) {
		t.Error("FT8_COLOR=always must force colour even with NO_COLOR set")
	}
	t.Setenv("FT8_COLOR", "never")
	if useColor(os.Stderr) {
		t.Error("FT8_COLOR=never must disable colour")
	}
	t.Setenv("FT8_COLOR", "")
	if useColor(os.Stderr) {
		t.Error("NO_COLOR must disable colour when FT8_COLOR is unset")
	}
}

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
