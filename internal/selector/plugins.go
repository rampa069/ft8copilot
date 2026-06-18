package selector

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/rampamac/ft8copilot/internal/config"
	"github.com/rampamac/ft8copilot/internal/db"
)

// This file ports the nine concrete call-selector plugins from
// FT8Commander's plugins/ directory. Each plugin embeds *Base for the shared
// candidate query and SelectRecord logic, parses its own config, and filters
// the candidate pool before delegating to SelectRecord.
//
// The Python XOR idiom `(cond) ^ self.reverse` is rendered in Go as
// `cond != reverse`.

// ---------------------------------------------------------------------------
// Any (plugins/any.py): no filtering.
// ---------------------------------------------------------------------------

type anyPlugin struct{ *Base }

func newAnyPlugin(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
	return &anyPlugin{Base: NewBase(name, cfg, deps)}, nil
}

func (p *anyPlugin) Get(band int) (Candidate, bool) {
	return p.SelectRecord(p.Candidates(band))
}

// ---------------------------------------------------------------------------
// CallSign (plugins/callsign.py): keep a candidate when its call is in the
// configured list, or (failing that) when it matches the configured regexp.
// ---------------------------------------------------------------------------

type callSignPlugin struct {
	*Base
	expr     *regexp.Regexp
	callList map[string]bool
	reverse  bool
}

func newCallSignPlugin(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
	expr, err := regexp.Compile(cfg.Regexp)
	if err != nil {
		return nil, fmt.Errorf("invalid regexp %q: %w", cfg.Regexp, err)
	}
	list := make(map[string]bool, len(cfg.List))
	for _, c := range cfg.List {
		list[c] = true
	}
	return &callSignPlugin{
		Base:     NewBase(name, cfg, deps),
		expr:     expr,
		callList: list,
		reverse:  cfg.Reverse,
	}, nil
}

func (p *callSignPlugin) Get(band int) (Candidate, bool) {
	var records []Candidate
	for _, rec := range p.Candidates(band) {
		// Python checks the list first, then the regexp (re.search ->
		// unanchored MatchString).
		if p.callList[rec.Call] != p.reverse {
			p.Log().Warn("select call from list", "call", rec.Call)
			records = append(records, rec)
		} else if p.expr.MatchString(rec.Call) != p.reverse {
			p.Log().Warn("select call from regexp", "call", rec.Call)
			records = append(records, rec)
		}
	}
	return p.SelectRecord(records)
}

// ---------------------------------------------------------------------------
// Grid (plugins/grid.py): keep a candidate when its grid matches the regexp.
// ---------------------------------------------------------------------------

type gridPlugin struct {
	*Base
	expr    *regexp.Regexp
	reverse bool
}

func newGridPlugin(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
	expr, err := regexp.Compile(cfg.Regexp)
	if err != nil {
		return nil, fmt.Errorf("invalid regexp %q: %w", cfg.Regexp, err)
	}
	return &gridPlugin{
		Base:    NewBase(name, cfg, deps),
		expr:    expr,
		reverse: cfg.Reverse,
	}, nil
}

func (p *gridPlugin) Get(band int) (Candidate, bool) {
	var records []Candidate
	for _, rec := range p.Candidates(band) {
		// Python catches TypeError when grid is None; here an empty grid is
		// skipped with a warning.
		if rec.Grid == "" {
			p.Log().Warn("unable to determine the grid", "message", rec.Packet.Message)
			continue
		}
		if p.expr.MatchString(rec.Grid) != p.reverse {
			records = append(records, rec)
		}
	}
	return p.SelectRecord(records)
}

// ---------------------------------------------------------------------------
// Continent (plugins/continent.py Continent): keep a candidate when its
// continent is in the configured (validated) set.
// ---------------------------------------------------------------------------

var validContinents = map[string]bool{
	"AF": true, "AS": true, "EU": true, "NA": true, "OC": true, "SA": true,
}

type continentPlugin struct {
	*Base
	set     map[string]bool
	reverse bool
}

func newContinentPlugin(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
	b := NewBase(name, cfg, deps)
	set := make(map[string]bool)
	for _, c := range cfg.List {
		if validContinents[c] {
			set[c] = true
		} else {
			b.Log().Warn("ignoring continent: not valid", "continent", c)
		}
	}
	return &continentPlugin{Base: b, set: set, reverse: cfg.Reverse}, nil
}

func (p *continentPlugin) Get(band int) (Candidate, bool) {
	var records []Candidate
	for _, rec := range p.Candidates(band) {
		if p.set[rec.Continent] != p.reverse {
			records = append(records, rec)
		}
	}
	return p.SelectRecord(records)
}

// ---------------------------------------------------------------------------
// Country (plugins/continent.py Country): keep a candidate when its country is
// in the configured set.
//
// Deviation from the Python original: continent.py validates each configured
// entity via DXEntity.isentity and warns on invalid ones. The framework here
// has no DXCC entity-name dependency available at selector-construction time,
// and the filter only requires string membership, so that validation is
// intentionally omitted.
// ---------------------------------------------------------------------------

type countryPlugin struct {
	*Base
	set     map[string]bool
	reverse bool
}

func newCountryPlugin(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
	set := make(map[string]bool, len(cfg.List))
	for _, c := range cfg.List {
		set[c] = true
	}
	return &countryPlugin{Base: NewBase(name, cfg, deps), set: set, reverse: cfg.Reverse}, nil
}

func (p *countryPlugin) Get(band int) (Candidate, bool) {
	var records []Candidate
	for _, rec := range p.Candidates(band) {
		if p.set[rec.Country] != p.reverse {
			records = append(records, rec)
		}
	}
	return p.SelectRecord(records)
}

// ---------------------------------------------------------------------------
// CQZone / ITUZone (plugins/zones.py): keep a candidate when its zone number is
// in the configured set.
//
// Two bugs in the Python original are corrected here:
//
//  1. zones.py's z_get iterates self.get(band), which recurses infinitely (get
//     calls z_get which calls get...). The INTENDED behavior is to iterate the
//     base candidate pool; we do that via b.Candidates(band).
//
//  2. zones.py builds a set of zone STRINGS but compares it against the int DB
//     value, so it never matches. We parse the configured zone strings to ints
//     (skipping non-integers with a warning, mirroring the Python int(zone)
//     guard) and compare int-against-int.
// ---------------------------------------------------------------------------

// zoneField selects which zone field of a candidate a zoneSelector compares.
type zoneField func(db.Record) int

func cqZoneOf(r db.Record) int  { return r.CQZone }
func ituZoneOf(r db.Record) int { return r.ITUZone }

type zonePlugin struct {
	*Base
	set     map[int]bool
	field   zoneField
	reverse bool
}

func newZonePlugin(field zoneField) Constructor {
	return func(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
		b := NewBase(name, cfg, deps)
		set := make(map[int]bool)
		for _, z := range cfg.List {
			n, err := strconv.Atoi(z)
			if err != nil {
				b.Log().Warn("zone is not an integer", "selector", name, "zone", z)
				continue
			}
			set[n] = true
		}
		return &zonePlugin{Base: b, set: set, field: field, reverse: cfg.Reverse}, nil
	}
}

func (p *zonePlugin) Get(band int) (Candidate, bool) {
	var records []Candidate
	for _, rec := range p.Candidates(band) {
		if p.set[p.field(rec.Record)] != p.reverse {
			records = append(records, rec)
		}
	}
	return p.SelectRecord(records)
}

// ---------------------------------------------------------------------------
// DXCC100 (plugins/special.py DXCC100): keep a candidate only if its country is
// NOT already worked worked_count times on the band.
// ---------------------------------------------------------------------------

type dxcc100Plugin struct {
	*Base
	store       *db.Store
	workedCount int
}

func newDXCC100Plugin(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
	workedCount := cfg.WorkedCount
	if workedCount == 0 {
		workedCount = 2 // Python getattr(..., "worked_count", 2)
	}
	return &dxcc100Plugin{
		Base:        NewBase(name, cfg, deps),
		store:       deps.Store,
		workedCount: workedCount,
	}, nil
}

func (p *dxcc100Plugin) Get(band int) (Candidate, bool) {
	worked, err := p.store.WorkedCountries(band, p.workedCount)
	if err != nil {
		p.Log().Error("worked countries query failed", "band", band, "err", err)
		return Candidate{}, false
	}
	workedSet := make(map[string]bool, len(worked))
	for _, c := range worked {
		workedSet[c] = true
	}
	var records []Candidate
	for _, rec := range p.Candidates(band) {
		if !workedSet[rec.Country] {
			p.Log().Debug("selected", "call", rec.Call)
			records = append(records, rec)
		}
	}
	return p.SelectRecord(records)
}

// ---------------------------------------------------------------------------
// Extra (plugins/special.py Extra): keep a candidate when its Extra field is in
// the configured set.
// ---------------------------------------------------------------------------

type extraPlugin struct {
	*Base
	set     map[string]bool
	reverse bool
}

func newExtraPlugin(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
	set := make(map[string]bool, len(cfg.List))
	for _, e := range cfg.List {
		set[e] = true
	}
	return &extraPlugin{Base: NewBase(name, cfg, deps), set: set, reverse: cfg.Reverse}, nil
}

func (p *extraPlugin) Get(band int) (Candidate, bool) {
	var records []Candidate
	for _, rec := range p.Candidates(band) {
		if p.set[rec.Extra] != p.reverse {
			records = append(records, rec)
		}
	}
	return p.SelectRecord(records)
}

// ---------------------------------------------------------------------------
// Registration.
// ---------------------------------------------------------------------------

func init() {
	Register("Any", newAnyPlugin)
	Register("CallSign", newCallSignPlugin)
	Register("Grid", newGridPlugin)
	Register("Continent", newContinentPlugin)
	Register("Country", newCountryPlugin)
	Register("CQZone", newZonePlugin(cqZoneOf))
	Register("ITUZone", newZonePlugin(ituZoneOf))
	Register("DXCC100", newDXCC100Plugin)
	Register("Extra", newExtraPlugin)
}
