package tui

import (
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	applog "github.com/rampa069/ft8copilot/internal/log"
)

// logMsg delivers one new log entry from the sink subscription to the model.
type logMsg applog.Entry

// defaultLogBufferLines bounds how many entries the view keeps in memory; the
// underlying sink ring keeps the authoritative history.
const defaultLogBufferLines = 1000

// logView is the scrollback model for the log window: it seeds from the sink's
// snapshot, appends live entries from the subscription, and renders the tail.
// It is embedded by the root model and placed in a panel by the layout task.
type logView struct {
	ch      <-chan applog.Entry
	cancel  func()
	entries []applog.Entry
	max     int
}

// newLogView subscribes to the sink and seeds the buffer with recent history. A
// nil sink yields an inert view (no channel, empty render).
func newLogView(sink *applog.Sink, max int) logView {
	if max <= 0 {
		max = defaultLogBufferLines
	}
	lv := logView{max: max}
	if sink == nil {
		return lv
	}
	lv.entries = sink.Snapshot()
	if len(lv.entries) > max {
		lv.entries = append([]applog.Entry(nil), lv.entries[len(lv.entries)-max:]...)
	}
	lv.ch, lv.cancel = sink.Subscribe()
	return lv
}

// listen returns a command that blocks for the next log entry. Re-issue it after
// each logMsg to keep streaming. Returns nil when there is no subscription or it
// has been closed.
func (l logView) listen() tea.Cmd {
	ch := l.ch
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return logMsg(e)
	}
}

// add appends an entry, trimming to the buffer cap.
func (l *logView) add(e applog.Entry) {
	l.entries = append(l.entries, e)
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
}

// close unsubscribes from the sink.
func (l *logView) close() {
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
		l.ch = nil
	}
}

// last returns the most recent entry and whether one exists.
func (l logView) last() (applog.Entry, bool) {
	if len(l.entries) == 0 {
		return applog.Entry{}, false
	}
	return l.entries[len(l.entries)-1], true
}

// render draws the tail of the log to fit width×height, one entry per line:
// "HH:MM:SS LEVEL message", with the timestamp dimmed and the level coloured.
func (l logView) render(width, height int) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	start := 0
	if len(l.entries) > height {
		start = len(l.entries) - height
	}
	lines := make([]string, 0, height)
	for _, e := range l.entries[start:] {
		lines = append(lines, l.formatEntry(e, width))
	}
	// Pad to full height so the panel interior is filled.
	for len(lines) < height {
		lines = append(lines, fit("", width))
	}
	return strings.Join(lines, "\n")
}

func (l logView) formatEntry(e applog.Entry, width int) string {
	ts := stLogTime.Render(e.Time.Format("15:04:05"))
	lvl := levelStyle(e.Level).Render(levelLabel(e.Level))
	prefix := ts + " " + lvl + " "
	// Width budget for the message after the fixed "HH:MM:SS LEVEL " prefix
	// (8 + 1 + 5 + 1 = 15 visible columns).
	msgWidth := width - 15
	if msgWidth < 1 {
		msgWidth = 1
	}
	msg := stLogMsg.Render(fit(e.Message, msgWidth))
	return prefix + msg
}

// Log-line styles, on the panel background.
var (
	stLogTime = lipgloss.NewStyle().Foreground(colDim).Background(colPanel)
	stLogMsg  = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
)

// levelLabel returns a fixed-width 5-char level label so lines align.
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

func levelStyle(l slog.Level) lipgloss.Style {
	s := lipgloss.NewStyle().Background(colPanel).Bold(true)
	switch {
	case l < slog.LevelInfo:
		return s.Foreground(colDim)
	case l < slog.LevelWarn:
		return s.Foreground(colRunning) // green
	case l < slog.LevelError:
		return s.Foreground(colTitle) // yellow
	default:
		return s.Foreground(colPaused) // red
	}
}
