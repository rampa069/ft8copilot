package selector

import (
	"testing"

	"github.com/rampa069/ft8copilot/internal/blacklist"
)

func newRankerForTest(h *harness, deps Deps) *Ranker {
	if deps.Store == nil {
		deps.Store = h.store
	}
	if deps.Blacklist == nil {
		deps.Blacklist = blacklist.New(nil)
	}
	r := NewRanker(deps)
	r.CacheTTL = 0 // reflect each insert immediately
	return r
}

func TestRankSortsAndAnnotates(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "G3XYZ", "", -3)   // best SNR, eligible -> chosen
	h.insert(t, "W5XX", "", -8)    // blacklisted
	h.insert(t, "CO8LY", "", -10)  // eligible, not chosen
	h.insert(t, "JA1ABC", "", -50) // SNR at the lower bound -> ineligible

	r := newRankerForTest(h, Deps{Blacklist: blacklist.New([]string{"W5XX"})})
	ranked := r.Rank(20)

	if len(ranked) != 4 {
		t.Fatalf("ranked len = %d, want 4", len(ranked))
	}

	wantOrder := []string{"G3XYZ", "W5XX", "CO8LY", "JA1ABC"}
	for i, w := range wantOrder {
		if ranked[i].Call != w {
			t.Errorf("position %d = %q, want %q (SNR-desc order)", i, ranked[i].Call, w)
		}
	}

	checks := map[string]struct {
		eligible bool
		reason   string
		chosen   bool
	}{
		"G3XYZ":  {true, "", true},
		"W5XX":   {false, ReasonBlacklist, false},
		"CO8LY":  {true, "", false},
		"JA1ABC": {false, ReasonSNR, false},
	}
	for _, rk := range ranked {
		want := checks[rk.Call]
		if rk.Eligible != want.eligible {
			t.Errorf("%s eligible = %v, want %v", rk.Call, rk.Eligible, want.eligible)
		}
		if rk.Reason != want.reason {
			t.Errorf("%s reason = %q, want %q", rk.Call, rk.Reason, want.reason)
		}
		if rk.Chosen != want.chosen {
			t.Errorf("%s chosen = %v, want %v", rk.Call, rk.Chosen, want.chosen)
		}
	}
}

func TestRankEmptyPool(t *testing.T) {
	h := newHarness(t)
	r := newRankerForTest(h, Deps{})
	if got := r.Rank(20); len(got) != 0 {
		t.Errorf("empty pool rank len = %d, want 0", len(got))
	}
}

func TestRankChosenFollowsWiredPick(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "G3XYZ", "", -3)  // highest SNR, top permissive pick
	h.insert(t, "CO8LY", "", -10) // what the (stricter) chain would actually call

	r := newRankerForTest(h, Deps{})
	// Simulate a configured chain whose tighter rules pick the lower-SNR CO8LY.
	var gotBand int
	r.SetPick(func(band int) (Selection, bool) {
		gotBand = band
		c := Candidate{}
		c.Call = "CO8LY"
		return Selection{Candidate: c, Selector: "DXCC100"}, true
	})

	ranked := r.Rank(20)
	if gotBand != 20 {
		t.Errorf("pick called with band %d, want 20", gotBand)
	}
	if ranked[0].Call != "G3XYZ" || ranked[0].Chosen {
		t.Errorf("permissive leader G3XYZ must be listed but not chosen, got chosen=%v", ranked[0].Chosen)
	}
	if ranked[1].Call != "CO8LY" || !ranked[1].Chosen {
		t.Errorf("chain's pick CO8LY should be chosen, got %+v", ranked[1])
	}
}

func TestRankNoChosenWhenChainDeclines(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "G3XYZ", "", -3)

	r := newRankerForTest(h, Deps{})
	r.SetPick(func(int) (Selection, bool) { return Selection{}, false })

	for _, rk := range r.Rank(20) {
		if rk.Chosen {
			t.Errorf("%s chosen, but chain declined — no row should be chosen", rk.Call)
		}
	}
}

func TestRankChosenSkipsIneligibleLeader(t *testing.T) {
	h := newHarness(t)
	h.insert(t, "W5XX", "", -2)   // highest SNR but blacklisted
	h.insert(t, "CO8LY", "", -12) // first eligible -> chosen

	r := newRankerForTest(h, Deps{Blacklist: blacklist.New([]string{"W5XX"})})
	ranked := r.Rank(20)
	if ranked[0].Call != "W5XX" || ranked[0].Chosen {
		t.Errorf("leader W5XX should be listed first but not chosen, got chosen=%v", ranked[0].Chosen)
	}
	if !ranked[1].Chosen || ranked[1].Call != "CO8LY" {
		t.Errorf("CO8LY should be the chosen station, got %+v", ranked[1])
	}
}
