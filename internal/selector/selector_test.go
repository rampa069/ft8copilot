package selector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rampa069/ft8copilot/internal/blacklist"
	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/dxcc"
)

// anySelector is the minimal selector used to exercise the base logic: it runs
// the unfiltered candidate pool straight through SelectRecord (the "Any" plugin
// behavior).
type anySelector struct{ *Base }

func (s anySelector) Get(band int) (Candidate, bool) {
	return s.SelectRecord(s.Candidates(band))
}

// fakeMembership is an injectable LOTW set for tests.
type fakeMembership map[string]bool

func (f fakeMembership) Contains(call string) bool { return f[call] }

// harness builds a temp store + writer for inserting spots.
type harness struct {
	store  *db.Store
	writer *db.Writer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "sel.sqlite"))
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
	return &harness{store: store, writer: w}
}

func (h *harness) insert(t *testing.T, call, extra string, snr int32) {
	t.Helper()
	spot := db.Spot{
		Call:      call,
		Extra:     extra,
		Grid:      "FN20",
		Frequency: 14074000,
		Band:      20,
		Packet:    db.Packet{Time: time.Now().UTC(), SNR: snr, Mode: "~", Message: "CQ " + call},
	}
	if err := h.writer.Process(db.InsertCmd{Spot: spot}); err != nil {
		t.Fatalf("insert %s: %v", call, err)
	}
}

// newAny builds an anySelector over the harness store with the given config,
// caching disabled so each query reflects the latest inserts.
func newAny(h *harness, name string, cfg config.SelectorConfig, deps Deps) anySelector {
	if deps.Store == nil {
		deps.Store = h.store
	}
	if deps.Blacklist == nil {
		deps.Blacklist = blacklist.New(nil)
	}
	b := NewBase(name, cfg, deps)
	b.CacheTTL = 0
	return anySelector{Base: b}
}

func TestSelectRecordSortsBySNR(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "CO8LY", "", -10)  // Cuba
	h.insert(t, "G3XYZ", "", -3)   // England, best SNR
	h.insert(t, "JA1ABC", "", -15) // Japan

	sel := newAny(h, "Any", config.SelectorConfig{}, Deps{})
	got, ok := sel.Get(20)
	if !ok {
		t.Fatal("expected a selection")
	}
	if got.Call != "G3XYZ" {
		t.Errorf("selected %s, want G3XYZ (highest SNR)", got.Call)
	}
	if got.Coef == 0 {
		t.Error("coefficient not computed")
	}
}

func TestSelectRecordBlacklist(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "CO8LY", "", -10)
	h.insert(t, "G3XYZ", "", -3)

	sel := newAny(h, "Any", config.SelectorConfig{}, Deps{Blacklist: blacklist.New([]string{"G3XYZ"})})
	got, ok := sel.Get(20)
	if !ok {
		t.Fatal("expected a selection")
	}
	if got.Call != "CO8LY" {
		t.Errorf("selected %s, want CO8LY (G3XYZ blacklisted)", got.Call)
	}
}

func TestSelectRecordSNRBounds(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "CO8LY", "", -10)
	h.insert(t, "G3XYZ", "", 5)

	minSNR, maxSNR := -8, 0
	cfg := config.SelectorConfig{MinSNR: &minSNR, MaxSNR: &maxSNR}
	sel := newAny(h, "Any", cfg, Deps{})
	// G3XYZ (5) is above maxSNR; CO8LY (-10) is below minSNR -> nothing passes.
	if _, ok := sel.Get(20); ok {
		t.Fatal("expected no selection within bounds (-8,0)")
	}

	minSNR2 := -12
	cfg2 := config.SelectorConfig{MinSNR: &minSNR2, MaxSNR: &maxSNR}
	sel2 := newAny(h, "Any", cfg2, Deps{})
	got, ok := sel2.Get(20)
	if !ok || got.Call != "CO8LY" {
		t.Errorf("got (%v,%v), want CO8LY", got.Call, ok)
	}
}

func TestCandidatesDXContinentFilter(t *testing.T) {
	h := newHarness(t)
	// CO8LY is NA; with my_continent=NA and extra=DX it must be ignored.
	h.insert(t, "CO8LY", "DX", -3)
	h.insert(t, "G3XYZ", "", -10) // EU, kept

	sel := newAny(h, "Any", config.SelectorConfig{}, Deps{})
	cands := sel.Candidates(20)
	if len(cands) != 1 || cands[0].Call != "G3XYZ" {
		t.Fatalf("candidates = %+v, want only G3XYZ", calls(cands))
	}
}

func TestCandidatesContinentFromDeps(t *testing.T) {
	h := newHarness(t)
	// CO8LY is NA calling CQ DX. For a EU operator (Deps.Continent=EU) it must
	// be kept; the hardcoded default would have wrongly dropped it as "own
	// continent".
	h.insert(t, "CO8LY", "DX", -3)

	sel := newAny(h, "Any", config.SelectorConfig{}, Deps{Continent: "EU"})
	cands := sel.Candidates(20)
	if len(cands) != 1 || cands[0].Call != "CO8LY" {
		t.Fatalf("candidates = %+v, want CO8LY kept for EU operator", calls(cands))
	}
}

func TestPerSelectorContinentOverridesDeps(t *testing.T) {
	h := newHarness(t)
	// Per-selector my_continent (NA) wins over Deps.Continent (EU): a NA station
	// calling CQ DX is dropped again.
	h.insert(t, "CO8LY", "DX", -3)

	cfg := config.SelectorConfig{MyContinent: "NA"}
	sel := newAny(h, "Any", cfg, Deps{Continent: "EU"})
	if cands := sel.Candidates(20); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none (per-selector NA filter)", calls(cands))
	}
}

func TestLOTWFilter(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "CO8LY", "", -10)
	h.insert(t, "G3XYZ", "", -3) // best SNR but not an LOTW user

	cfg := config.SelectorConfig{LOTWUsersOnly: true}
	deps := Deps{LOTW: fakeMembership{"CO8LY": true}}
	sel := newAny(h, "Any", cfg, deps)
	got, ok := sel.Get(20)
	if !ok {
		t.Fatal("expected a selection")
	}
	if got.Call != "CO8LY" {
		t.Errorf("selected %s, want CO8LY (only LOTW user)", got.Call)
	}
}

func TestChainSelect(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "CO8LY", "", -10)

	// First selector rejects everything (impossible SNR window); second accepts.
	maxSNR := -100
	reject := newAny(h, "Reject", config.SelectorConfig{MaxSNR: &maxSNR}, Deps{})
	accept := newAny(h, "Accept", config.SelectorConfig{}, Deps{})

	chain := Chain{reject, accept}
	got, ok := chain.Select(20)
	if !ok {
		t.Fatal("expected a selection from the chain")
	}
	if got.Selector != "Accept" {
		t.Errorf("selector = %q, want Accept", got.Selector)
	}
	if got.Call != "CO8LY" {
		t.Errorf("call = %q, want CO8LY", got.Call)
	}
}

func TestBuildUnknownSelector(t *testing.T) {
	if _, err := Build([]string{"DoesNotExist"}, nil, Deps{}); err == nil {
		t.Fatal("expected error for unknown selector")
	}
}

func TestBuildRegistered(t *testing.T) {
	Register("AnyTest", func(name string, cfg config.SelectorConfig, deps Deps) (Selector, error) {
		return anySelector{Base: NewBase(name, cfg, deps)}, nil
	})
	if !Registered("AnyTest") {
		t.Fatal("AnyTest not registered")
	}
	h := newHarness(t)
	h.insert(t, "CO8LY", "", -10)
	deps := Deps{Store: h.store, Blacklist: blacklist.New(nil)}
	chain, err := Build([]string{"AnyTest"}, nil, deps)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(chain) != 1 || chain[0].Name() != "AnyTest" {
		t.Fatalf("chain = %v, want [AnyTest]", chain)
	}
}

func calls(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Call
	}
	return out
}
