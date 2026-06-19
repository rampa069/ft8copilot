// Package control is the in-process surface the TUI uses to change daemon
// parameters live, without sending SIGHUP or editing the YAML file. It mirrors
// the SIGHUP reload path in cmd/ft8ctrl: it rebuilds the selector chain and
// hands the hot-reloadable settings to the running Sequencer, and updates the
// purge interval atomic.
//
// Only hot-reloadable parameters are exposed (tx_power, tx_retries,
// follow_frequency, retry_time, the selector chain and the blacklist). The
// socket/database/identity fields are immutable at runtime, exactly as for
// SIGHUP.
package control

import (
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/rampa069/ft8copilot/internal/blacklist"
	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/selector"
	"github.com/rampa069/ft8copilot/internal/sequencer"
)

// Params are the editable, hot-reloadable daemon parameters.
type Params struct {
	TXPower         int
	TXRetries       int
	FollowFrequency bool
	ConsiderRR73    bool
	RetryTime       int // minutes
	CallSelector    []string
	BlackList       []string
}

// Deps are the live components a Controller needs to apply changes.
type Deps struct {
	Store      *db.Store
	Members    selector.Membership // LOTW set (may be nil)
	Continent  string              // operator's own continent
	Seq        *sequencer.Sequencer
	RetryNanos *atomic.Int64
	Logger     *slog.Logger
}

// Controller applies parameter changes to the running daemon. It is safe for
// concurrent use.
type Controller struct {
	mu   sync.Mutex
	cfg  *config.Config // current effective configuration
	deps Deps
}

// New builds a Controller over the current configuration and live components.
func New(cfg *config.Config, deps Deps) *Controller {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Controller{cfg: cfg, deps: deps}
}

// Params returns the current editable parameters.
func (c *Controller) Params() Params {
	c.mu.Lock()
	defer c.mu.Unlock()
	ft := c.cfg.FT8Ctrl
	return Params{
		TXPower:         ft.TXPower,
		TXRetries:       ft.TXRetries,
		FollowFrequency: ft.FollowFrequency,
		ConsiderRR73:    ft.ConsiderRR73,
		RetryTime:       ft.RetryTime,
		CallSelector:    append([]string(nil), ft.CallSelector...),
		BlackList:       append([]string(nil), c.cfg.BlackList...),
	}
}

// Apply validates and applies new parameters: it rebuilds the selector chain
// from the new chain order and blacklist, hands the sequencer settings to the
// running Sequencer, and updates the purge interval. On any error (e.g. an
// unknown selector) nothing is changed and the error is returned.
func (c *Controller) Apply(p Params) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Work on a copy so a failed build leaves the effective config untouched.
	newCfg := *c.cfg
	newCfg.FT8Ctrl.TXPower = p.TXPower
	newCfg.FT8Ctrl.TXRetries = p.TXRetries
	newCfg.FT8Ctrl.FollowFrequency = p.FollowFrequency
	newCfg.FT8Ctrl.ConsiderRR73 = p.ConsiderRR73
	newCfg.FT8Ctrl.RetryTime = p.RetryTime
	newCfg.FT8Ctrl.CallSelector = append([]string(nil), p.CallSelector...)
	newCfg.BlackList = blacklist.Normalize(p.BlackList)

	chain, err := selector.Build(newCfg.FT8Ctrl.CallSelector, newCfg.Selectors, selector.Deps{
		Store:     c.deps.Store,
		Blacklist: blacklist.New(newCfg.BlackList),
		LOTW:      c.deps.Members,
		Continent: c.deps.Continent,
		Log:       c.deps.Logger,
	})
	if err != nil {
		return err
	}

	if c.deps.Seq != nil {
		c.deps.Seq.Reload(newCfg.FT8Ctrl, chain)
	}
	if c.deps.RetryNanos != nil {
		c.deps.RetryNanos.Store(int64(time.Duration(p.RetryTime) * time.Minute))
	}

	*c.cfg = newCfg
	c.deps.Logger.Info("parameters applied via TUI",
		"tx_power", p.TXPower, "tx_retries", p.TXRetries,
		"follow_frequency", p.FollowFrequency, "consider_rr73", p.ConsiderRR73,
		"retry_time", p.RetryTime, "chain", newCfg.FT8Ctrl.CallSelector)
	return nil
}
