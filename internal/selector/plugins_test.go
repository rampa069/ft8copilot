package selector

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rampa069/ft8copilot/internal/blacklist"
	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/dxcc"
)

// pluginHarness is like the selector_test.go harness but also remembers the
// db file path so tests can open a raw connection for direct SQL tweaks (used
// to create a row with an empty grid, which the Writer would otherwise skip).
type pluginHarness struct {
	store  *db.Store
	writer *db.Writer
	path   string
}

func newPluginHarness(t *testing.T) *pluginHarness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sel.sqlite")
	store, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	entities, err := dxcc.New()
	if err != nil {
		t.Fatalf("dxcc.New: %v", err)
	}
	w, err := db.NewWriter(store, "FM18", entities, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	return &pluginHarness{store: store, writer: w, path: path}
}

// insert adds a status=0 spot on band 20 with grid FN20.
func (h *pluginHarness) insert(t *testing.T, call, extra string, snr int32) {
	t.Helper()
	h.insertGrid(t, call, extra, snr, "FN20")
}

func (h *pluginHarness) insertGrid(t *testing.T, call, extra string, snr int32, grid string) {
	t.Helper()
	spot := db.Spot{
		Call:      call,
		Extra:     extra,
		Grid:      grid,
		Frequency: 14074000,
		Band:      20,
		Packet:    db.Packet{Time: time.Now().UTC(), SNR: snr, Mode: "~", Message: "CQ " + call},
	}
	if err := h.writer.Process(db.InsertCmd{Spot: spot}); err != nil {
		t.Fatalf("insert %s: %v", call, err)
	}
}

// markWorked sets a row's status to 2 (logged) on band 20.
func (h *pluginHarness) markWorked(t *testing.T, call string) {
	t.Helper()
	if err := h.writer.Process(db.StatusCmd{Call: call, Band: 20, Status: 2}); err != nil {
		t.Fatalf("status %s: %v", call, err)
	}
}

// rawExec opens a separate connection to the same sqlite file and runs a
// statement. Used to blank a grid, which the Writer cannot produce.
func (h *pluginHarness) rawExec(t *testing.T, query string, args ...any) {
	t.Helper()
	conn, err := sql.Open("sqlite", h.path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// build constructs a registered plugin with caching disabled and the harness
// store/blacklist wired in.
func (h *pluginHarness) build(t *testing.T, name string, cfg config.SelectorConfig, deps Deps) Selector {
	t.Helper()
	if deps.Store == nil {
		deps.Store = h.store
	}
	if deps.Blacklist == nil {
		deps.Blacklist = blacklist.New(nil)
	}
	ctor := registryGet(t, name)
	sel, err := ctor(name, cfg, deps)
	if err != nil {
		t.Fatalf("construct %s: %v", name, err)
	}
	disableCache(sel)
	return sel
}

func registryGet(t *testing.T, name string) Constructor {
	t.Helper()
	ctor, ok := registry[name]
	if !ok {
		t.Fatalf("selector %q not registered", name)
	}
	return ctor
}

// disableCache sets the embedded Base.CacheTTL to 0 so each Get re-queries.
func disableCache(sel Selector) {
	switch p := sel.(type) {
	case *anyPlugin:
		p.CacheTTL = 0
	case *callSignPlugin:
		p.CacheTTL = 0
	case *gridPlugin:
		p.CacheTTL = 0
	case *continentPlugin:
		p.CacheTTL = 0
	case *countryPlugin:
		p.CacheTTL = 0
	case *zonePlugin:
		p.CacheTTL = 0
	case *dxcc100Plugin:
		p.CacheTTL = 0
	case *extraPlugin:
		p.CacheTTL = 0
	}
}

func TestCallSignByList(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "", -3)  // best SNR, not in list
	h.insert(t, "G3XYZ", "", -10) // in list

	// Regexp matches nothing so only the list governs selection (an empty
	// regexp would match every call, like Python re.compile("")).
	cfg := config.SelectorConfig{Regexp: "ZZZZ", List: config.StringList{"G3XYZ"}}
	sel := h.build(t, "CallSign", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ", got.Call, ok)
	}
}

func TestCallSignByRegexp(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "", -3)
	h.insert(t, "G3XYZ", "", -10)

	cfg := config.SelectorConfig{Regexp: "^G"}
	sel := h.build(t, "CallSign", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (^G regexp)", got.Call, ok)
	}
}

func TestCallSignReverse(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "", -3)  // best SNR, in list & matches ^C -> excluded by reverse
	h.insert(t, "G3XYZ", "", -10) // not in list, no ^C match -> kept by reverse

	// With reverse, both branches invert: CO8LY (in list, matches ^C) is
	// rejected, G3XYZ (in neither) is kept.
	cfg := config.SelectorConfig{Regexp: "^C", List: config.StringList{"CO8LY"}, Reverse: true}
	sel := h.build(t, "CallSign", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (reverse ^C)", got.Call, ok)
	}
}

func TestCallSignBadRegexp(t *testing.T) {
	h := newPluginHarness(t)
	cfg := config.SelectorConfig{Regexp: "("}
	ctor := registryGet(t, "CallSign")
	if _, err := ctor("CallSign", cfg, Deps{Store: h.store, Blacklist: blacklist.New(nil)}); err == nil {
		t.Fatal("expected error for invalid regexp")
	}
}

func TestGridByRegexp(t *testing.T) {
	h := newPluginHarness(t)
	h.insertGrid(t, "CO8LY", "", -3, "EL98")  // best SNR, grid not ^FN
	h.insertGrid(t, "G3XYZ", "", -10, "FN20") // grid matches ^FN

	cfg := config.SelectorConfig{Regexp: "^FN"}
	sel := h.build(t, "Grid", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (^FN grid)", got.Call, ok)
	}
}

func TestGridEmptyGridSkipped(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "G3XYZ", "", -10) // grid FN20, matches
	h.insert(t, "CO8LY", "", -3)  // will have its grid blanked
	// Blank CO8LY's grid directly; the Writer would never store an empty grid.
	h.rawExec(t, "UPDATE cqcalls SET grid = '' WHERE call = ?", "CO8LY")

	cfg := config.SelectorConfig{Regexp: "^FN"}
	sel := h.build(t, "Grid", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (empty-grid CO8LY skipped)", got.Call, ok)
	}
}

func TestContinentInclude(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "JA1ABC", "", -3) // AS, best SNR
	h.insert(t, "G3XYZ", "", -10) // EU
	h.insert(t, "CO8LY", "", -20) // NA

	cfg := config.SelectorConfig{List: config.StringList{"EU"}}
	sel := h.build(t, "Continent", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (EU only)", got.Call, ok)
	}
}

func TestContinentReverse(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "JA1ABC", "", -3) // AS, best SNR, excluded by reverse(EU)? no -> kept
	h.insert(t, "G3XYZ", "", -10) // EU -> excluded by reverse

	cfg := config.SelectorConfig{List: config.StringList{"EU"}, Reverse: true}
	sel := h.build(t, "Continent", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "JA1ABC" {
		t.Fatalf("got (%q,%v), want JA1ABC (reverse EU)", got.Call, ok)
	}
}

func TestContinentInvalidDropped(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "G3XYZ", "", -10) // EU

	// "XX" is not a valid continent and must be dropped; only "EU" remains.
	cfg := config.SelectorConfig{List: config.StringList{"XX", "EU"}}
	sel := h.build(t, "Continent", cfg, Deps{})
	cp := sel.(*continentPlugin)
	if cp.set["XX"] {
		t.Error("invalid continent XX was not dropped")
	}
	if !cp.set["EU"] {
		t.Error("valid continent EU missing")
	}
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ", got.Call, ok)
	}
}

func TestCountryInclude(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "", -10) // Cuba
	h.insert(t, "G3XYZ", "", -3)  // England, best SNR

	cfg := config.SelectorConfig{List: config.StringList{"Cuba"}}
	sel := h.build(t, "Country", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "CO8LY" {
		t.Fatalf("got (%q,%v), want CO8LY (Cuba only)", got.Call, ok)
	}
}

func TestCountryReverse(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "", -3)  // Cuba, best SNR -> excluded by reverse
	h.insert(t, "G3XYZ", "", -10) // England -> kept

	cfg := config.SelectorConfig{List: config.StringList{"Cuba"}, Reverse: true}
	sel := h.build(t, "Country", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (reverse Cuba)", got.Call, ok)
	}
}

func TestCQZoneMatch(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "", -10) // Cuba
	h.insert(t, "G3XYZ", "", -3)  // England, best SNR

	// Read back the actual CQ zone enriched into CO8LY's row, then select it.
	rec, ok, err := h.store.GetCall("CO8LY")
	if err != nil || !ok {
		t.Fatalf("GetCall CO8LY: (%v,%v)", ok, err)
	}
	if rec.CQZone == 0 {
		t.Fatal("CO8LY has no CQ zone enriched")
	}
	// Sanity: G3XYZ must be in a different CQ zone for the test to discriminate.
	g, _, _ := h.store.GetCall("G3XYZ")
	if g.CQZone == rec.CQZone {
		t.Skipf("G3XYZ and CO8LY share CQ zone %d", rec.CQZone)
	}

	cfg := config.SelectorConfig{List: config.StringList{strconv.Itoa(rec.CQZone)}}
	sel := h.build(t, "CQZone", cfg, Deps{})
	got, ok := sel.Get(20)
	// Proves int-vs-int matching (the corrected zones.py bug): the higher-SNR
	// G3XYZ is filtered out, CO8LY is selected by its int CQ zone.
	if !ok || got.Call != "CO8LY" {
		t.Fatalf("got (%q,%v), want CO8LY (CQ zone %d)", got.Call, ok, rec.CQZone)
	}
}

func TestITUZoneMatch(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "", -10) // Cuba
	h.insert(t, "G3XYZ", "", -3)  // England, best SNR

	rec, ok, err := h.store.GetCall("CO8LY")
	if err != nil || !ok {
		t.Fatalf("GetCall CO8LY: (%v,%v)", ok, err)
	}
	if rec.ITUZone == 0 {
		t.Fatal("CO8LY has no ITU zone enriched")
	}
	g, _, _ := h.store.GetCall("G3XYZ")
	if g.ITUZone == rec.ITUZone {
		t.Skipf("G3XYZ and CO8LY share ITU zone %d", rec.ITUZone)
	}

	cfg := config.SelectorConfig{List: config.StringList{strconv.Itoa(rec.ITUZone)}}
	sel := h.build(t, "ITUZone", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "CO8LY" {
		t.Fatalf("got (%q,%v), want CO8LY (ITU zone %d)", got.Call, ok, rec.ITUZone)
	}
}

func TestCQZoneNonIntegerDropped(t *testing.T) {
	h := newPluginHarness(t)
	cfg := config.SelectorConfig{List: config.StringList{"foo", "14"}}
	sel := h.build(t, "CQZone", cfg, Deps{})
	zp := sel.(*zonePlugin)
	if zp.set[14] != true || len(zp.set) != 1 {
		t.Fatalf("zone set = %v, want only {14}", zp.set)
	}
}

func TestDXCC100ExcludesWorked(t *testing.T) {
	h := newPluginHarness(t)
	// Work Cuba twice (worked_count default 2): insert + mark status=2.
	h.insert(t, "CO8LY", "", -1)
	h.markWorked(t, "CO8LY")
	h.insert(t, "CO2AB", "", -1)
	h.markWorked(t, "CO2AB")

	// Now a fresh Cuban spot (worked) and an English spot (not worked).
	h.insert(t, "CO5XX", "", -3)  // Cuba, best SNR, but worked -> excluded
	h.insert(t, "G3XYZ", "", -10) // England, not worked -> selected

	cfg := config.SelectorConfig{} // worked_count defaults to 2
	sel := h.build(t, "DXCC100", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (Cuba already worked)", got.Call, ok)
	}
}

func TestExtraInclude(t *testing.T) {
	h := newPluginHarness(t)
	// Extra "WANTED" set; the DX-continent filter only drops Extra=="DX" on own
	// continent, so a custom Extra value is safe.
	h.insert(t, "CO8LY", "WANTED", -10)
	h.insert(t, "G3XYZ", "", -3) // best SNR but Extra not in set

	cfg := config.SelectorConfig{List: config.StringList{"WANTED"}}
	sel := h.build(t, "Extra", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "CO8LY" {
		t.Fatalf("got (%q,%v), want CO8LY (Extra WANTED)", got.Call, ok)
	}
}

func TestExtraReverse(t *testing.T) {
	h := newPluginHarness(t)
	h.insert(t, "CO8LY", "WANTED", -3) // best SNR, in set -> excluded by reverse
	h.insert(t, "G3XYZ", "", -10)      // Extra "" not in set -> kept by reverse

	cfg := config.SelectorConfig{List: config.StringList{"WANTED"}, Reverse: true}
	sel := h.build(t, "Extra", cfg, Deps{})
	got, ok := sel.Get(20)
	if !ok || got.Call != "G3XYZ" {
		t.Fatalf("got (%q,%v), want G3XYZ (reverse WANTED)", got.Call, ok)
	}
}

func TestBuildAllNine(t *testing.T) {
	h := newPluginHarness(t)
	names := []string{"Any", "CallSign", "Grid", "Continent", "Country", "CQZone", "ITUZone", "DXCC100", "Extra"}
	for _, n := range names {
		if !Registered(n) {
			t.Errorf("%q not registered", n)
		}
	}

	// CallSign and Grid require a valid regexp in their config sections.
	cfgs := map[string]config.SelectorConfig{
		"CallSign": {Regexp: ".*"},
		"Grid":     {Regexp: ".*"},
	}
	deps := Deps{Store: h.store, Blacklist: blacklist.New(nil)}
	chain, err := Build(names, cfgs, deps)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(chain) != len(names) {
		t.Fatalf("chain length %d, want %d", len(chain), len(names))
	}
	for i, n := range names {
		if chain[i].Name() != n {
			t.Errorf("chain[%d].Name() = %q, want %q", i, chain[i].Name(), n)
		}
	}
}
