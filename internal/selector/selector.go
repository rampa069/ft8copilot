package selector

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/rampamac/ft8copilot/internal/blacklist"
	"github.com/rampamac/ft8copilot/internal/config"
	"github.com/rampamac/ft8copilot/internal/db"
)

// nowFunc is the clock, overridable in tests.
var nowFunc = time.Now

// Membership reports whether a callsign belongs to a set (e.g. LOTW users).
type Membership interface {
	Contains(call string) bool
}

// everything is a Membership that accepts every callsign. It stands in for the
// Python "Nothing" class used when a selector is not restricted to LOTW users.
type everything struct{}

func (everything) Contains(string) bool { return true }

// Candidate is a cqcalls row in the running for selection, annotated with the
// distance/SNR coefficient. The coefficient mirrors the original (it is computed
// for every candidate) even though selection orders by SNR, not coefficient.
type Candidate struct {
	db.Record
	Coef float64
}

// Selection is the result of running a selector chain: the chosen candidate and
// the name of the selector that picked it (Python set data['selector']).
type Selection struct {
	Candidate
	Selector string
}

// Selector scores the spots on a band and optionally returns the best one to
// call. Implementations embed *Base for the shared query and filtering logic.
type Selector interface {
	Name() string
	Get(band int) (Candidate, bool)
}

// Chain is an ordered list of selectors; the first one to return a candidate
// wins. It replaces the Python LoadPlugins.__call__.
type Chain []Selector

// Select runs the chain for a band and returns the first candidate produced.
func (c Chain) Select(band int) (Selection, bool) {
	for _, s := range c {
		if cand, ok := s.Get(band); ok {
			return Selection{Candidate: cand, Selector: s.Name()}, true
		}
	}
	return Selection{}, false
}

// Deps are the shared dependencies handed to every selector constructor.
type Deps struct {
	Store     *db.Store
	Blacklist *blacklist.Blacklist
	// LOTW is the live LOTW membership. It is used only by selectors configured
	// with lotw_users_only; may be nil if LOTW could not be loaded.
	LOTW Membership
	Log  *slog.Logger
}

// Constructor builds a selector from its config section and the shared deps.
type Constructor func(name string, cfg config.SelectorConfig, deps Deps) (Selector, error)

// registry maps a selector's config-section name to its constructor. Plugins
// register themselves via Register (replacing Python's dynamic import).
var registry = map[string]Constructor{}

// Register adds a selector constructor under name. Typically called from a
// plugin file's init().
func Register(name string, c Constructor) { registry[name] = c }

// Registered reports whether a selector name has a constructor.
func Registered(name string) bool {
	_, ok := registry[name]
	return ok
}

// Build resolves the configured selector names into a Chain, in order. cfgs is
// the per-selector config map (config.Config.Selectors); a name with no section
// gets a zero SelectorConfig (all defaults).
func Build(names []string, cfgs map[string]config.SelectorConfig, deps Deps) (Chain, error) {
	if deps.Log == nil {
		deps.Log = slog.Default()
	}
	var chain Chain
	for _, name := range names {
		ctor, ok := registry[name]
		if !ok {
			return nil, fmt.Errorf("selector: unknown call_selector %q", name)
		}
		sel, err := ctor(name, cfgs[name], deps)
		if err != nil {
			return nil, fmt.Errorf("selector %q: %w", name, err)
		}
		chain = append(chain, sel)
	}
	return chain, nil
}
