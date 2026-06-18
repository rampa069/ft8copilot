// Package integration exercises the real wsjtx -> db -> selector pipeline
// end-to-end without a network socket, proving the packages compose the way the
// ft8ctrl command wires them together.
package integration

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/rampamac/ft8copilot/internal/blacklist"
	"github.com/rampamac/ft8copilot/internal/config"
	"github.com/rampamac/ft8copilot/internal/db"
	"github.com/rampamac/ft8copilot/internal/dxcc"
	"github.com/rampamac/ft8copilot/internal/selector"
)

// quietLog returns a logger that discards output so test logs stay clean.
func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newPipeline opens a temp store and builds a Writer with a real DXCC resolver
// and the FM18 origin grid, mirroring the wiring in cmd/ft8ctrl.
func newPipeline(t *testing.T) (*db.Store, *db.Writer) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "integration.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	entities, err := dxcc.New()
	if err != nil {
		t.Fatalf("dxcc.New: %v", err)
	}
	w, err := db.NewWriter(store, "FM18", entities, quietLog())
	if err != nil {
		t.Fatalf("db.NewWriter: %v", err)
	}
	return store, w
}

// cqSpot builds a db.Spot the way the sequencer does after parsing a CQ Decode:
// the Packet carries the decoded fields and the Spot carries the enrichment
// inputs (call, grid, frequency, band).
func cqSpot(call, grid string, snr int32, at time.Time) db.Spot {
	return db.Spot{
		Call:      call,
		Extra:     "",
		Grid:      grid,
		Frequency: 14074000,
		Band:      20,
		Packet: db.Packet{
			New:     true,
			Time:    at,
			SNR:     snr,
			Mode:    "~", // FT8
			Message: "CQ " + call + " " + grid,
		},
	}
}

// TestPipelineSelectsBestStation drives the full wsjtx->db->selector pipeline:
// two CQ spots are inserted via the Writer (as the sequencer would after parsing
// a CQ Decode datagram), an "Any" selector chain is built via selector.Build,
// and the higher-SNR station must win. Then the winner is marked worked and must
// drop out of the selectable pool while showing up in WorkedCountries.
func TestPipelineSelectsBestStation(t *testing.T) {
	store, w := newPipeline(t)
	now := time.Now().UTC().Truncate(time.Second)

	// CO8LY = Cuba (NA), G3XYZ = England (EU). G3XYZ has the better SNR.
	if err := w.Process(db.InsertCmd{Spot: cqSpot("CO8LY", "FL20", -12, now)}); err != nil {
		t.Fatalf("insert CO8LY: %v", err)
	}
	if err := w.Process(db.InsertCmd{Spot: cqSpot("G3XYZ", "IO91", -4, now)}); err != nil {
		t.Fatalf("insert G3XYZ: %v", err)
	}

	// Sanity: both rows landed with the expected DXCC enrichment.
	if rec, ok, _ := store.GetCall("CO8LY"); !ok || rec.Country != "Cuba" {
		t.Fatalf("CO8LY not stored as Cuba: ok=%v rec=%+v", ok, rec)
	}
	if rec, ok, _ := store.GetCall("G3XYZ"); !ok || rec.Continent != "EU" {
		t.Fatalf("G3XYZ not stored as EU: ok=%v rec=%+v", ok, rec)
	}

	deps := selector.Deps{
		Store:     store,
		Blacklist: blacklist.New(nil),
		LOTW:      nil,
		Log:       quietLog(),
	}
	// Use the default (empty) per-selector config so SNR bounds are wide open.
	chain, err := selector.Build([]string{"Any"}, map[string]config.SelectorConfig{}, deps)
	if err != nil {
		t.Fatalf("selector.Build: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("chain length = %d, want 1", len(chain))
	}

	sel, ok := chain.Select(20)
	if !ok {
		t.Fatal("chain.Select(20) returned no candidate")
	}
	if sel.Call != "G3XYZ" {
		t.Errorf("selected %q, want G3XYZ (higher SNR -4 > -12)", sel.Call)
	}
	if sel.Selector != "Any" {
		t.Errorf("selector name = %q, want Any", sel.Selector)
	}

	// Mark the winner worked (status=2). It must leave the selectable pool and
	// appear in WorkedCountries.
	if err := w.Process(db.StatusCmd{Call: "G3XYZ", Band: 20, Status: 2}); err != nil {
		t.Fatalf("status worked: %v", err)
	}

	// Build a fresh chain for the next selection cycle. The Base caches its
	// candidate pool per band for a few seconds, so re-selecting on the same
	// chain instance within the TTL would return the stale pool; the sequencer
	// re-queries on a longer cadence, which a new chain models here.
	chain2, err := selector.Build([]string{"Any"}, map[string]config.SelectorConfig{}, deps)
	if err != nil {
		t.Fatalf("selector.Build (2): %v", err)
	}
	sel2, ok := chain2.Select(20)
	if !ok {
		t.Fatal("chain.Select(20) after worked returned nothing, want CO8LY")
	}
	if sel2.Call != "CO8LY" {
		t.Errorf("after marking G3XYZ worked, selected %q, want CO8LY", sel2.Call)
	}

	worked, err := store.WorkedCountries(20, 1)
	if err != nil {
		t.Fatalf("WorkedCountries: %v", err)
	}
	if len(worked) != 1 || worked[0] != "England" {
		t.Errorf("WorkedCountries = %v, want [England]", worked)
	}

	// The worked station is no longer in the recent/unworked pool.
	recent, err := store.Recent(20, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 1 || recent[0].Call != "CO8LY" {
		t.Errorf("Recent pool = %v, want only CO8LY", callsOf(recent))
	}
}

// TestConfigRoundTripWiresSelector loads the shipped sample config and feeds its
// Selectors map into selector.Build for the sample's call_selector (["Any"]),
// proving the exact construction path the main command uses actually wires up.
func TestConfigRoundTripWiresSelector(t *testing.T) {
	cfg, err := config.Load("../../ft8ctrl.yaml.sample")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// The sample's call_selector is ["Any"].
	if len(cfg.FT8Ctrl.CallSelector) != 1 || cfg.FT8Ctrl.CallSelector[0] != "Any" {
		t.Fatalf("call_selector = %v, want [Any]", cfg.FT8Ctrl.CallSelector)
	}
	// The sample configures Any with min_snr/max_snr; make sure those parsed so
	// Build receives a non-default section.
	anyCfg, ok := cfg.Selectors["Any"]
	if !ok {
		t.Fatal("sample config has no Any selector section")
	}
	if anyCfg.MinSNR == nil || anyCfg.MaxSNR == nil {
		t.Fatalf("Any section min/max not parsed: %+v", anyCfg)
	}
	if *anyCfg.MinSNR != -18 || *anyCfg.MaxSNR != 3 {
		t.Errorf("Any min/max = %d/%d, want -18/3", *anyCfg.MinSNR, *anyCfg.MaxSNR)
	}

	store, _ := newPipeline(t)
	deps := selector.Deps{
		Store:     store,
		Blacklist: blacklist.New(cfg.BlackList),
		LOTW:      nil, // sample sets lotw_users_only:true, but LOTW unavailable -> accept all
		Log:       quietLog(),
	}

	// This is the wiring path the main command will use:
	// selector.Build(cfg.FT8Ctrl.CallSelector, cfg.Selectors, deps).
	chain, err := selector.Build([]string(cfg.FT8Ctrl.CallSelector), cfg.Selectors, deps)
	if err != nil {
		t.Fatalf("selector.Build from sample config: %v", err)
	}
	if len(chain) != 1 || chain[0].Name() != "Any" {
		t.Fatalf("chain = %v, want [Any]", chain)
	}

	// Empty store: the chain constructs and runs without panicking, returning no
	// candidate.
	if _, ok := chain.Select(20); ok {
		t.Error("empty store yielded a selection, want none")
	}
}

func callsOf(recs []db.Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Call
	}
	return out
}
