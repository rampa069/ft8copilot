package selector

import (
	"sort"

	"github.com/rampamac/ft8copilot/internal/config"
)

// ranker.go provides the candidate-ranking API used by the TUI's "candidates in
// order" view (FT8CoPilot-yjm). Where a Chain returns only the single station the
// autopilot would call next, a Ranker returns the whole pool for a band, ranked
// best-to-worst, with each candidate annotated by why it is (or is not) callable.
//
// The Ranker shares the selectors' base scoring: the same recent-spots query,
// the same own-continent-DX filter, the same distance/SNR coefficient, and the
// same SNR-bound / blacklist / LOTW eligibility checks as SelectRecord. It uses
// the framework's default SNR bounds and accepts every callsign for LOTW (a lone
// selector with no lotw_users_only), so eligibility here means "callable by a
// permissive selector"; a configured chain with tighter bounds may decline a
// candidate the Ranker marks eligible.

// Ineligible reasons, reported in Ranked.Reason (empty when eligible).
const (
	ReasonSNR       = "snr"       // outside the [min,max] SNR window
	ReasonBlacklist = "blacklist" // callsign is blacklisted
	ReasonLOTW      = "lotw"      // not a registered LOTW user (when required)
)

// Ranked is a candidate annotated for display: the scored record, whether it is
// currently callable, the reason it is not (when ineligible), and whether it is
// the top eligible station (the one a permissive selector would call now).
type Ranked struct {
	Candidate
	Eligible bool
	Reason   string
	Chosen   bool
}

// Ranker ranks the candidate pool for a band. Build one with NewRanker from the
// same Deps used to build the selector Chain.
type Ranker struct {
	*Base
}

// NewRanker builds a Ranker over the shared deps, using the framework's default
// SNR bounds and recent-spots window.
func NewRanker(deps Deps) *Ranker {
	return &Ranker{Base: NewBase("ranker", config.SelectorConfig{}, deps)}
}

// Rank returns the band's candidate pool sorted by SNR (descending), each
// annotated with eligibility. The highest-SNR eligible candidate is marked
// Chosen. The slice is empty when no spots are in the window.
func (r *Ranker) Rank(band int) []Ranked {
	cands := r.Candidates(band)
	sorted := make([]Candidate, len(cands))
	copy(sorted, cands)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].SNR > sorted[j].SNR
	})

	out := make([]Ranked, 0, len(sorted))
	chosen := false
	for _, c := range sorted {
		eligible, reason := r.eligibility(c)
		rk := Ranked{Candidate: c, Eligible: eligible, Reason: reason}
		if eligible && !chosen {
			rk.Chosen = true
			chosen = true
		}
		out = append(out, rk)
	}
	return out
}

// eligibility mirrors the per-record checks in SelectRecord: SNR bounds, then
// blacklist, then LOTW membership.
func (r *Ranker) eligibility(c Candidate) (bool, string) {
	snr := int(c.SNR)
	if !(r.minSNR < snr && snr < r.maxSNR) {
		return false, ReasonSNR
	}
	if r.blacklist.Contains(c.Call) {
		return false, ReasonBlacklist
	}
	if !r.lotw.Contains(c.Call) {
		return false, ReasonLOTW
	}
	return true, ""
}
