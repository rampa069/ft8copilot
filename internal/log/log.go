package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Logfile rotation defaults, matching ft8ctrl.py (RotatingFileHandler, 2 MiB,
// 5 backups).
const (
	logfileMaxBytes = 2 << 20
	logfileBackups  = 5
)

// Setup builds the application logger: a console handler at the level given by
// the LOG_LEVEL environment variable (default INFO), and — when logfile is
// non-empty — a size-rotating file handler at DEBUG level. It returns the logger
// and a Closer for the file (a no-op when there is no file).
func Setup(logfile string) (*slog.Logger, io.Closer) {
	level := parseLevel(os.Getenv("LOG_LEVEL"))

	handlers := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}),
	}
	var closer io.Closer = noopCloser{}

	if logfile != "" {
		rw, err := newRotatingWriter(expandHome(logfile), logfileMaxBytes, logfileBackups)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: log file %q disabled: %v\n", logfile, err)
		} else {
			handlers = append(handlers,
				slog.NewTextHandler(rw, &slog.HandlerOptions{Level: slog.LevelDebug}))
			closer = rw
		}
	}

	return slog.New(&fanout{handlers: handlers}), closer
}

// parseLevel maps a LOG_LEVEL string to a slog level, defaulting to INFO.
func parseLevel(s string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// fanout dispatches each record to every wrapped handler that is enabled for the
// record's level, letting the console and file handlers filter independently.
type fanout struct{ handlers []slog.Handler }

func (f *fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f *fanout) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range f.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (f *fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &fanout{handlers: next}
}

func (f *fanout) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithGroup(name)
	}
	return &fanout{handlers: next}
}

// rotatingWriter is a minimal size-based rotating file writer (path, path.1, …,
// path.N), replacing Python's RotatingFileHandler without an external dependency.
type rotatingWriter struct {
	mu      sync.Mutex
	path    string
	max     int64
	backups int
	file    *os.File
	size    int64
}

func newRotatingWriter(path string, max int64, backups int) (*rotatingWriter, error) {
	w := &rotatingWriter{path: path, max: max, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > w.max {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	// Drop the oldest, then shift each backup up by one.
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.backups))
	for i := w.backups - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
	}
	_ = os.Rename(w.path, w.path+".1")
	return w.open()
}

// Close closes the underlying file.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// expandHome expands a leading ~ to the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
