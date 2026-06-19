package selector

import (
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/rampa069/ft8copilot/internal/blacklist"
	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
)

// Defaults mirroring plugins/base.py.
const (
	defaultMinSNR    = -50
	defaultMaxSNR    = 50
	defaultDelta     = 29 * time.Second
	defaultContinent = "NA"
	cacheTTL         = 3 * time.Second // SingleObjectCache maxage
)

// Base provides the shared selector behavior: the recent-spots query (cached
// per band for a few seconds), the DX-to-own-continent filter, the SNR/distance
// coefficient, and the final sort + SNR/blacklist/LOTW selection. Concrete
// selectors embed *Base and implement Get.
type Base struct {
	name      string
	store     *db.Store
	blacklist *blacklist.Blacklist
	lotw      Membership
	minSNR    int
	maxSNR    int
	delta     time.Duration
	continent string
	log       *slog.Logger

	// CacheTTL bounds how long Candidates results are reused per band. Defaults
	// to cacheTTL; set to 0 to disable caching (used by tests).
	CacheTTL time.Duration
	cache    bandCache
}

// NewBase builds the shared Base for a selector from its config and deps. It is
// exported so plugin constructors can compose it.
func NewBase(name string, cfg config.SelectorConfig, deps Deps) *Base {
	minSNR := defaultMinSNR
	if cfg.MinSNR != nil {
		minSNR = *cfg.MinSNR
	}
	maxSNR := defaultMaxSNR
	if cfg.MaxSNR != nil {
		maxSNR = *cfg.MaxSNR
	}
	delta := defaultDelta
	if cfg.Delta > 0 {
		delta = time.Duration(cfg.Delta) * time.Second
	}
	// Own-continent default chain: the hardcoded fallback, then the operator's
	// continent from Deps (global ft8ctrl.my_continent or derived from my_call),
	// then a per-selector my_continent override with the highest priority.
	continent := defaultContinent
	if deps.Continent != "" {
		continent = deps.Continent
	}
	if cfg.MyContinent != "" {
		continent = cfg.MyContinent
	}

	// Restrict to LOTW users only when configured (and LOTW is available);
	// otherwise accept everyone, like the Python "Nothing".
	var member Membership = everything{}
	if cfg.LOTWUsersOnly && deps.LOTW != nil {
		member = deps.LOTW
	}

	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	if cfg.Debug {
		log = log.With("selector", name)
	}

	return &Base{
		name:      name,
		store:     deps.Store,
		blacklist: deps.Blacklist,
		lotw:      member,
		minSNR:    minSNR,
		maxSNR:    maxSNR,
		delta:     delta,
		continent: continent,
		log:       log,
		CacheTTL:  cacheTTL,
	}
}

// Name returns the selector's configured name.
func (b *Base) Name() string { return b.name }

// Log exposes the selector's logger to embedding plugins.
func (b *Base) Log() *slog.Logger { return b.log }

// Reverse-helper accessors are intentionally omitted; plugins read their own
// config (regexp/list/reverse/worked_count) directly.

// Candidates returns the current pool of selectable spots on a band: status=0
// rows seen within delta, with the DX-to-own-continent entries removed and the
// distance/SNR coefficient attached. Results are cached per band for CacheTTL.
func (b *Base) Candidates(band int) []Candidate {
	return b.cache.get(band, b.CacheTTL, func() []Candidate {
		since := nowFunc().UTC().Add(-b.delta)
		recs, err := b.store.Recent(band, since)
		if err != nil {
			b.log.Error("recent query failed", "band", band, "err", err)
			return nil
		}
		out := make([]Candidate, 0, len(recs))
		for _, r := range recs {
			if r.Extra == "DX" && r.Continent == b.continent {
				b.log.Warn("ignoring DX calling own continent",
					"call", r.Call, "continent", r.Continent)
				continue
			}
			out = append(out, Candidate{Record: r, Coef: coefficient(r.Distance, r.SNR)})
		}
		return out
	})
}

// SelectRecord sorts candidates by SNR (descending) and returns the first that
// passes the SNR bounds, is not blacklisted, and is a LOTW user (when required).
// Port of select_record in plugins/base.py.
func (b *Base) SelectRecord(records []Candidate) (Candidate, bool) {
	sorted := make([]Candidate, len(records))
	copy(sorted, records)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].SNR > sorted[j].SNR
	})
	for _, rec := range sorted {
		snr := int(rec.SNR)
		if !(b.minSNR < snr && snr < b.maxSNR) {
			continue
		}
		if b.blacklist.Contains(rec.Call) {
			b.log.Debug("blacklisted", "call", rec.Call)
			continue
		}
		if !b.lotw.Contains(rec.Call) {
			b.log.Debug("not an LOTW user", "call", rec.Call)
			continue
		}
		return rec, true
	}
	return Candidate{}, false
}

// coefficient = distance * 10^(snr/10). Static helper from plugins/base.py.
func coefficient(dist float64, snr int32) float64 {
	return dist * math.Pow(10, float64(snr)/10)
}

// bandCache caches Candidates results per band for a short TTL, replacing the
// Python SingleObjectCache (which, due to a quirk, shared one cache across all
// bands; this keys correctly by band).
type bandCache struct {
	mu      sync.Mutex
	entries map[int]cacheEntry
}

type cacheEntry struct {
	data []Candidate
	at   time.Time
}

func (c *bandCache) get(band int, ttl time.Duration, fill func() []Candidate) []Candidate {
	if ttl <= 0 {
		return fill()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := nowFunc()
	if c.entries != nil {
		if e, ok := c.entries[band]; ok && now.Sub(e.at) < ttl {
			return e.data
		}
	}
	data := fill()
	if c.entries == nil {
		c.entries = make(map[int]cacheEntry)
	}
	c.entries[band] = cacheEntry{data: data, at: now}
	return data
}
