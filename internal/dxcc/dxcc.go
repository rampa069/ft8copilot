package dxcc

import (
	_ "embed"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/cty.dat
var ctyData string

// ErrNotFound is returned when a callsign/prefix cannot be resolved to any DXCC
// entity, or when a requested entity name does not exist.
var ErrNotFound = errors.New("dxcc: no matching entity")

// Entity describes a resolved DXCC entity for a callsign or prefix lookup.
type Entity struct {
	Prefix    string // the matching prefix (or exact call) that resolved the lookup
	Country   string // DXCC entity / country name
	Continent string // two-letter continent code (e.g. NA, EU, AS, OC)
	CQZone    int    // CQ zone
	ITUZone   int    // ITU zone
}

// alias is a single prefix or exact-call entry with optional per-alias zone /
// continent overrides applied on top of its owning country's defaults.
type alias struct {
	prefix    string
	country   string
	continent string
	cqZone    int
	ituZone   int
}

// DXCC holds the parsed cty.dat data and provides lookup operations.
type DXCC struct {
	exact   map[string]alias // =CALL exact-match entries keyed by full callsign
	prefix  map[string]alias // prefix entries keyed by the bare prefix string
	country map[string][]string
}

var (
	defaultOnce sync.Once
	defaultDX   *DXCC
	defaultErr  error
)

// New parses the embedded cty.dat and returns a ready-to-use DXCC. The result is
// cached, so repeated calls are cheap and share the same parsed data.
func New() (*DXCC, error) {
	defaultOnce.Do(func() {
		defaultDX, defaultErr = parse(ctyData)
	})
	return defaultDX, defaultErr
}

// parse builds a DXCC from the raw cty.dat content.
func parse(data string) (*DXCC, error) {
	d := &DXCC{
		exact:   make(map[string]alias),
		prefix:  make(map[string]alias),
		country: make(map[string][]string),
	}

	// cty.dat records are terminated by ';'. Normalise line endings, then split
	// on the terminator so each record is processed as one logical block.
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")

	for _, record := range strings.Split(data, ";") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		if err := d.parseRecord(record); err != nil {
			return nil, err
		}
	}

	if len(d.prefix) == 0 && len(d.exact) == 0 {
		return nil, errors.New("dxcc: cty.dat contained no entities")
	}
	return d, nil
}

func (d *DXCC) parseRecord(record string) error {
	// The header is the first line (8 colon-separated fields); everything after
	// the first newline is the comma-separated alias list.
	nl := strings.IndexByte(record, '\n')
	header := record
	aliasBlob := ""
	if nl >= 0 {
		header = record[:nl]
		aliasBlob = record[nl+1:]
	}

	fields := strings.Split(header, ":")
	if len(fields) < 8 {
		// Not a valid header; skip silently to be tolerant of stray content.
		return nil
	}

	country := strings.TrimSpace(fields[0])
	cqZone := atoiSafe(fields[1])
	ituZone := atoiSafe(fields[2])
	continent := strings.TrimSpace(fields[3])
	primary := strings.TrimSpace(fields[7])
	// A leading '*' marks a non-DXCC entity (e.g. a club); strip it.
	primary = strings.TrimPrefix(primary, "*")

	base := alias{
		country:   country,
		continent: continent,
		cqZone:    cqZone,
		ituZone:   ituZone,
	}

	var prefixes []string

	// The primary prefix from the header is itself a valid prefix entry.
	if primary != "" {
		a := base
		a.prefix = primary
		d.addPrefix(a)
		prefixes = append(prefixes, primary)
	}

	// Parse the comma-separated alias list.
	aliasBlob = strings.ReplaceAll(aliasBlob, "\n", "")
	for _, raw := range strings.Split(aliasBlob, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		a, exact := parseAlias(raw, base)
		if a.prefix == "" {
			continue
		}
		if exact {
			d.exact[a.prefix] = a
		} else {
			d.addPrefix(a)
		}
		prefixes = append(prefixes, a.prefix)
	}

	if country != "" {
		d.country[country] = append(d.country[country], prefixes...)
	}
	return nil
}

// addPrefix records a prefix alias, keeping the longest/most-specific override.
// Duplicate bare prefixes can legitimately appear; the first wins, which matches
// cty.dat ordering (more specific entities are listed where intended).
func (d *DXCC) addPrefix(a alias) {
	if _, ok := d.prefix[a.prefix]; !ok {
		d.prefix[a.prefix] = a
	}
}

// parseAlias parses one alias token, applying any override tokens. It returns the
// resulting alias and whether it is an exact-call match (leading '=').
func parseAlias(token string, base alias) (alias, bool) {
	a := base
	exact := false

	if strings.HasPrefix(token, "=") {
		exact = true
		token = token[1:]
	}

	// Pull out override tokens wherever they appear, leaving the bare prefix.
	var b strings.Builder
	for i := 0; i < len(token); {
		switch token[i] {
		case '(': // (n) CQ zone override
			if j := strings.IndexByte(token[i:], ')'); j >= 0 {
				a.cqZone = atoiSafe(token[i+1 : i+j])
				i += j + 1
				continue
			}
		case '[': // [n] ITU zone override
			if j := strings.IndexByte(token[i:], ']'); j >= 0 {
				a.ituZone = atoiSafe(token[i+1 : i+j])
				i += j + 1
				continue
			}
		case '<': // <lat/lon> coordinate override (ignored)
			if j := strings.IndexByte(token[i:], '>'); j >= 0 {
				i += j + 1
				continue
			}
		case '{': // {Continent} override
			if j := strings.IndexByte(token[i:], '}'); j >= 0 {
				a.continent = strings.TrimSpace(token[i+1 : i+j])
				i += j + 1
				continue
			}
		case '~': // ~ref~ time-offset override (ignored)
			if j := strings.IndexByte(token[i+1:], '~'); j >= 0 {
				i += j + 2
				continue
			}
		}
		b.WriteByte(token[i])
		i++
	}

	a.prefix = strings.ToUpper(strings.TrimSpace(b.String()))
	return a, exact
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// Lookup resolves a callsign (or prefix) to its DXCC entity. It first tries an
// exact-call match, then falls back to the longest matching prefix.
func (d *DXCC) Lookup(call string) (Entity, error) {
	call = strings.ToUpper(strings.TrimSpace(call))
	if call == "" {
		return Entity{}, ErrNotFound
	}

	if a, ok := d.exact[call]; ok {
		return a.entity(), nil
	}

	bestLen := -1
	var best alias
	for i := len(call); i > 0; i-- {
		if a, ok := d.prefix[call[:i]]; ok {
			bestLen = i
			best = a
			break
		}
	}

	if bestLen < 0 {
		return Entity{}, ErrNotFound
	}
	return best.entity(), nil
}

func (a alias) entity() Entity {
	return Entity{
		Prefix:    a.prefix,
		Country:   a.country,
		Continent: a.continent,
		CQZone:    a.cqZone,
		ITUZone:   a.ituZone,
	}
}

// Entities returns the sorted, unique list of DXCC entity (country) names.
func (d *DXCC) Entities() []string {
	names := make([]string, 0, len(d.country))
	for name := range d.country {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsEntity reports whether name is a known DXCC entity (country) name.
func (d *DXCC) IsEntity(name string) bool {
	_, ok := d.country[name]
	return ok
}

// GetEntity returns the sorted, unique list of prefixes/aliases belonging to the
// named DXCC entity. It returns ErrNotFound if the name is unknown.
func (d *DXCC) GetEntity(name string) ([]string, error) {
	prefixes, ok := d.country[name]
	if !ok {
		return nil, ErrNotFound
	}
	seen := make(map[string]struct{}, len(prefixes))
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
