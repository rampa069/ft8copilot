package log

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultSinkCapacity is how many recent log entries the TUI ring buffer keeps.
const defaultSinkCapacity = 500

// Entry is one captured log record, formatted for display. The message already
// includes the rendered attributes ("msg key=value …"); the TUI applies its own
// colour based on Level and Time, so no ANSI is stored here.
type Entry struct {
	Time    time.Time
	Level   slog.Level
	Message string
}

// Sink is an slog.Handler that keeps the most recent records in a bounded ring
// buffer and broadcasts each new record to live subscribers. It feeds the TUI
// log window. It never blocks the daemon: a subscriber whose channel is full
// simply misses entries (it can re-read the ring via Snapshot).
//
// The ring, subscriber set and lock live in a shared core so the handlers
// returned by WithAttrs/WithGroup all write to the same buffer.
type Sink struct {
	core  *sinkCore
	group string
	attrs string
}

type sinkCore struct {
	mu     sync.Mutex
	level  slog.Level
	ring   []Entry
	start  int // index of the oldest entry
	count  int
	subs   map[int]chan Entry
	nextID int
}

// NewSink builds a Sink that retains up to capacity entries and accepts records
// at or above level.
func NewSink(capacity int, level slog.Level) *Sink {
	if capacity <= 0 {
		capacity = defaultSinkCapacity
	}
	return &Sink{core: &sinkCore{
		level: level,
		ring:  make([]Entry, capacity),
		subs:  map[int]chan Entry{},
	}}
}

func (s *Sink) Enabled(_ context.Context, l slog.Level) bool {
	return l >= s.core.level
}

func (s *Sink) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	b.WriteString(s.attrs)
	r.Attrs(func(a slog.Attr) bool {
		appendPlainAttr(&b, s.group, a)
		return true
	})
	s.core.append(Entry{Time: r.Time, Level: r.Level, Message: b.String()})
	return nil
}

func (s *Sink) WithAttrs(as []slog.Attr) slog.Handler {
	if len(as) == 0 {
		return s
	}
	var b strings.Builder
	b.WriteString(s.attrs)
	for _, a := range as {
		appendPlainAttr(&b, s.group, a)
	}
	return &Sink{core: s.core, group: s.group, attrs: b.String()}
}

func (s *Sink) WithGroup(name string) slog.Handler {
	if name == "" {
		return s
	}
	return &Sink{core: s.core, group: s.group + name + ".", attrs: s.attrs}
}

// Snapshot returns a copy of the buffered entries, oldest first.
func (s *Sink) Snapshot() []Entry {
	c := s.core
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Entry, c.count)
	for i := 0; i < c.count; i++ {
		out[i] = c.ring[(c.start+i)%len(c.ring)]
	}
	return out
}

// Subscribe registers a listener and returns a channel of future entries plus an
// unsubscribe function. The channel is buffered; entries are dropped (never
// blocked) when the subscriber falls behind. Call the returned function to stop
// receiving and release the channel.
func (s *Sink) Subscribe() (<-chan Entry, func()) {
	c := s.core
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	ch := make(chan Entry, 256)
	c.subs[id] = ch
	return ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if sub, ok := c.subs[id]; ok {
			delete(c.subs, id)
			close(sub)
		}
	}
}

// append stores an entry in the ring (evicting the oldest when full) and
// broadcasts it to subscribers without blocking.
func (c *sinkCore) append(e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.ring)
	if c.count < n {
		c.ring[(c.start+c.count)%n] = e
		c.count++
	} else {
		c.ring[c.start] = e
		c.start = (c.start + 1) % n
	}
	for id, ch := range c.subs {
		select {
		case ch <- e:
		default:
			_ = id // subscriber is behind; drop rather than block the daemon
		}
	}
}

// appendPlainAttr renders one attribute as " key=value" without colour,
// flattening groups with a dotted prefix. It mirrors consoleHandler.appendAttr
// but emits no ANSI (the TUI colours entries itself).
func appendPlainAttr(b *strings.Builder, prefix string, a slog.Attr) {
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
			appendPlainAttr(b, next, ga)
		}
		return
	}
	if a.Equal(slog.Attr{}) {
		return
	}
	val := a.Value.String()
	if needsQuote(val) {
		val = strconv.Quote(val)
	}
	b.WriteByte(' ')
	b.WriteString(prefix + a.Key + "=" + val)
}
