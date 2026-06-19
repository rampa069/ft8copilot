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
	r.Base.CacheTTL = 0 // reflect each insert immediately
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
