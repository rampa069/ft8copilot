// Package blacklist provides a case-insensitive membership check for a set of
// blacklisted callsigns.
//
// It is a port of the BlackList class from FT8Commander's plugins/base.py. The
// Python original is a singleton tied to the global Config; this implementation
// is decoupled from config — the caller supplies the list of callsigns (which
// the config package already parses and uppercases from the BlackList YAML key).
package blacklist

import "strings"

// Blacklist holds a set of uppercased callsigns and reports membership.
type Blacklist struct {
	calls map[string]struct{}
}

// New builds a Blacklist from the given callsigns. Each entry is trimmed and
// uppercased defensively, so callers need not pre-normalize.
func New(calls []string) *Blacklist {
	m := make(map[string]struct{}, len(calls))
	for _, c := range calls {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		m[c] = struct{}{}
	}
	return &Blacklist{calls: m}
}

// Normalize trims and uppercases each callsign, dropping blanks. It mirrors how
// New canonicalises entries, for callers that need the cleaned list itself (e.g.
// to display or persist it).
func Normalize(calls []string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// Contains reports whether call is blacklisted. The query is trimmed and
// uppercased before lookup. A nil *Blacklist safely returns false.
func (b *Blacklist) Contains(call string) bool {
	if b == nil || b.calls == nil {
		return false
	}
	call = strings.ToUpper(strings.TrimSpace(call))
	_, ok := b.calls[call]
	return ok
}

// Len returns the number of blacklisted callsigns. A nil *Blacklist returns 0.
func (b *Blacklist) Len() int {
	if b == nil {
		return 0
	}
	return len(b.calls)
}
