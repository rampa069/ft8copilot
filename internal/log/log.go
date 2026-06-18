package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
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
		newConsoleHandler(os.Stderr, level, useColor(os.Stderr)),
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

// ANSI escape codes used by the console handler.
const (
	ansiReset    = "\x1b[0m"
	ansiDim      = "\x1b[2m"
	ansiGray     = "\x1b[90m"
	ansiGreen    = "\x1b[32m"
	ansiYellow   = "\x1b[33m"
	ansiRed      = "\x1b[31m"
	ansiBoldCyan = "\x1b[1;36m"
	ansiCyan     = "\x1b[36m"
)

// highlightKeys colours the values of the attributes most worth scanning for in
// live QSO activity. Other attributes keep the default colour.
var highlightKeys = map[string]string{
	"call":    ansiBoldCyan,
	"country": ansiCyan,
	"band":    ansiCyan,
}

// useColor decides whether to emit ANSI colour to w. FT8_COLOR=always|never
// forces the choice; otherwise NO_COLOR (if set) disables colour, and the
// default is "colour only when w is a terminal".
func useColor(w *os.File) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FT8_COLOR"))) {
	case "always", "force", "1", "yes", "on":
		return true
	case "never", "none", "0", "no", "off":
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return isatty.IsTerminal(w.Fd())
}

// consoleHandler is a slog.Handler that renders one compact, optionally coloured
// line per record: "HH:MM:SS LEVEL message key=value …". Colour is applied only
// when color is true, so the same type serves both a TTY and a plain pipe; the
// rotating file handler uses a separate plain TextHandler and is never coloured.
type consoleHandler struct {
	mu    *sync.Mutex
	out   io.Writer
	level slog.Leveler
	color bool
	group string // dotted prefix from WithGroup
	attrs string // attributes pre-rendered by WithAttrs
}

func newConsoleHandler(w io.Writer, level slog.Leveler, color bool) *consoleHandler {
	return &consoleHandler{mu: &sync.Mutex{}, out: w, level: level, color: color}
}

func (h *consoleHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level.Level()
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	ts := r.Time.Format("15:04:05")
	if h.color {
		b.WriteString(ansiDim + ts + ansiReset)
	} else {
		b.WriteString(ts)
	}
	b.WriteByte(' ')

	if h.color {
		b.WriteString(levelColor(r.Level) + levelLabel(r.Level) + ansiReset)
	} else {
		b.WriteString(levelLabel(r.Level))
	}
	b.WriteByte(' ')

	b.WriteString(r.Message)
	b.WriteString(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&b, h.group, a)
		return true
	})
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}

// appendAttr renders one attribute as " key=value", flattening groups with a
// dotted prefix and colouring the key (and highlighted values) when enabled.
func (h *consoleHandler) appendAttr(b *strings.Builder, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		grp := a.Value.Group()
		if len(grp) == 0 {
			return
		}
		next := prefix
		if a.Key != "" {
			next = prefix + a.Key + "."
		}
		for _, ga := range grp {
			h.appendAttr(b, next, ga)
		}
		return
	}
	if a.Equal(slog.Attr{}) {
		return
	}

	key := prefix + a.Key
	val := a.Value.String()
	if needsQuote(val) {
		val = strconv.Quote(val)
	}

	b.WriteByte(' ')
	if !h.color {
		b.WriteString(key + "=" + val)
		return
	}
	b.WriteString(ansiDim + key + "=" + ansiReset)
	if c, ok := highlightKeys[a.Key]; ok {
		b.WriteString(c + val + ansiReset)
	} else {
		b.WriteString(val)
	}
}

func (h *consoleHandler) WithAttrs(as []slog.Attr) slog.Handler {
	if len(as) == 0 {
		return h
	}
	nh := *h
	var b strings.Builder
	b.WriteString(h.attrs)
	for _, a := range as {
		h.appendAttr(&b, h.group, a)
	}
	nh.attrs = b.String()
	return &nh
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	nh := *h
	nh.group = h.group + name + "."
	return &nh
}

// levelLabel returns a fixed-width (5-char) level label so messages align.
func levelLabel(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO "
	case l < slog.LevelError:
		return "WARN "
	default:
		return "ERROR"
	}
}

func levelColor(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return ansiGray
	case l < slog.LevelWarn:
		return ansiGreen
	case l < slog.LevelError:
		return ansiYellow
	default:
		return ansiRed
	}
}

// needsQuote reports whether a value must be quoted to stay on one unambiguous
// field (empty, or containing whitespace, '=' or '"').
func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r <= ' ' || r == '=' || r == '"' {
			return true
		}
	}
	return false
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
