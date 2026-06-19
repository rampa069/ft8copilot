package selector

import (
	"sort"

	"github.com/rampa069/ft8copilot/internal/config"
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
//
// Eligibility colouring is therefore permissive, but the Chosen marker is not:
// when a PickFunc is wired (SetPick, normally the live Sequencer.Pick), Chosen
// flags the exact station the configured selector chain would call next, so the
// TUI highlight always matches what the autopilot does. Without a PickFunc the
// Ranker falls back to marking the top eligible candidate (standalone use).

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

// PickFunc reports the candidate a configured selector chain would call next on
// a band, or false when the chain declines. Sequencer.Pick satisfies it; the
// Ranker uses it to mark the Chosen candidate so the TUI highlight matches the
// autopilot. It must be safe to call concurrently.
type PickFunc func(band int) (Selection, bool)

// Ranker ranks the candidate pool for a band. Build one with NewRanker from the
// same Deps used to build the selector Chain.
type Ranker struct {
	*Base
	pick PickFunc // optional; when set, drives the Chosen marker (see SetPick)
}

// NewRanker builds a Ranker over the shared deps, using the framework's default
// SNR bounds and recent-spots window.
func NewRanker(deps Deps) *Ranker {
	return &Ranker{Base: NewBase("ranker", config.SelectorConfig{}, deps)}
}

// SetPick wires the live chain's selection so Rank marks the station the chain
// would actually call as Chosen, instead of the permissive top-eligible row.
// Pass the Sequencer's Pick method. Call once at startup before Rank is used.
func (r *Ranker) SetPick(pick PickFunc) { r.pick = pick }

// Rank returns the band's candidate pool sorted by SNR (descending), each
// annotated with eligibility. Exactly one candidate is marked Chosen: the
// station the wired chain would call next (SetPick), or — with no PickFunc —
// the highest-SNR eligible candidate. The slice is empty when no spots are in
// the window.
func (r *Ranker) Rank(band int) []Ranked {
	cands := r.Candidates(band)
	sorted := make([]Candidate, len(cands))
	copy(sorted, cands)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].SNR > sorted[j].SNR
	})

	out := make([]Ranked, 0, len(sorted))
	for _, c := range sorted {
		eligible, reason := r.eligibility(c)
		out = append(out, Ranked{Candidate: c, Eligible: eligible, Reason: reason})
	}
	r.markChosen(out, band)
	return out
}

// markChosen sets Chosen on at most one row. With a PickFunc it flags the row
// the chain would call (matched by callsign in SNR order, so the highest-SNR
// instance of a repeated call wins, exactly as the chain picks). If the chain
// declines, or its pick is no longer in the pool, no row is chosen. Without a
// PickFunc it falls back to the top eligible row.
func (r *Ranker) markChosen(out []Ranked, band int) {
	if r.pick != nil {
		sel, ok := r.pick(band)
		if !ok {
			return
		}
		for i := range out {
			if out[i].Call == sel.Call {
				out[i].Chosen = true
				return
			}
		}
		return
	}
	for i := range out {
		if out[i].Eligible {
			out[i].Chosen = true
			return
		}
	}
}

// eligibility mirrors the per-record checks in SelectRecord: SNR bounds, then
// blacklist, then LOTW membership.
func (r *Ranker) eligibility(c Candidate) (bool, string) {
	snr := int(c.SNR)
	if r.minSNR >= snr || snr >= r.maxSNR {
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
