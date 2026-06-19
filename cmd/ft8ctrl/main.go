// Command ft8ctrl is the WSJT-X FT8/FT4 automation daemon: it listens to
// WSJT-X UDP packets, records CQ-calling stations and automatically replies to
// the station with the best chance of completing a QSO.
//
// Port of ft8ctrl.py.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rampamac/ft8copilot/internal/blacklist"
	"github.com/rampamac/ft8copilot/internal/config"
	"github.com/rampamac/ft8copilot/internal/control"
	"github.com/rampamac/ft8copilot/internal/db"
	"github.com/rampamac/ft8copilot/internal/dxcc"
	applog "github.com/rampamac/ft8copilot/internal/log"
	"github.com/rampamac/ft8copilot/internal/lotw"
	"github.com/rampamac/ft8copilot/internal/selector"
	"github.com/rampamac/ft8copilot/internal/sequencer"
	"github.com/rampamac/ft8copilot/internal/tui"
)

// defaultLogfile mirrors LOGFILE_NAME in ft8ctrl.py.
const defaultLogfile = "ft8ctrl-debug.log"

// version is shown in the TUI header banner.
const version = "experimental"

// cmdQueue bounds the channel feeding the database writer.
const cmdQueue = 256

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ft8ctrl:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("c", "", "path to the configuration file (ft8ctrl.yaml)")
	flag.StringVar(configPath, "config", "", "path to the configuration file (ft8ctrl.yaml)")
	tuiMode := flag.Bool("tui", false, "run the interactive terminal UI (BlueBEEP front-end)")
	flag.Parse()

	// Resolve the config path once so SIGHUP can reload the same file.
	resolvedPath := *configPath
	if resolvedPath == "" {
		found, err := config.FindFile()
		if err != nil {
			return err
		}
		resolvedPath = found
	}
	cfg, err := config.Load(resolvedPath)
	if err != nil {
		return err
	}

	logfile := cfg.FT8Ctrl.LogfileName
	if logfile == "" {
		logfile = defaultLogfile
	}
	// In TUI mode the front-end owns the terminal, so suppress the stderr
	// console handler and route records to an in-memory sink for the log window
	// instead (the rotating file still captures everything).
	var (
		logger  *slog.Logger
		closer  io.Closer
		logSink *applog.Sink
	)
	if *tuiMode {
		logger, closer, logSink = applog.SetupTUI(logfile)
	} else {
		logger, closer = applog.Setup(logfile)
	}
	defer closer.Close()
	slog.SetDefault(logger)

	// Database + DXCC enrichment.
	store, err := db.Open(cfg.FT8Ctrl.DBName)
	if err != nil {
		return err
	}
	defer store.Close()
	logger.Info("database ready", "path", cfg.FT8Ctrl.DBName)

	entities, err := dxcc.New()
	if err != nil {
		return fmt.Errorf("dxcc: %w", err)
	}

	writer, err := db.NewWriter(store, cfg.FT8Ctrl.MyGrid, entities, logger)
	if err != nil {
		return fmt.Errorf("db writer: %w", err)
	}

	// Cancel everything on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmds := make(chan db.Command, cmdQueue)
	go writer.Run(ctx, cmds)

	// retryNanos holds the purge interval so SIGHUP can update retry_time live.
	var retryNanos atomic.Int64
	retryNanos.Store(int64(time.Duration(cfg.FT8Ctrl.RetryTime) * time.Minute))
	go runPurge(ctx, store, &retryNanos, logger)

	// Load the LOTW user list once, only if a selector actually needs it.
	var members selector.Membership
	if needsLOTW(cfg) {
		cache, err := lotw.Default()
		if err != nil {
			logger.Warn("LOTW unavailable, lotw_users_only selectors will accept everyone", "err", err)
		} else {
			members = cache
		}
	}

	ownContinent := operatorContinent(cfg, entities, logger)
	logger.Info("operator continent", "continent", ownContinent)

	selDeps := selector.Deps{
		Store:     store,
		Blacklist: blacklist.New(cfg.BlackList),
		LOTW:      members,
		Continent: ownContinent,
		Log:       logger,
	}
	chain, err := selector.Build(cfg.FT8Ctrl.CallSelector, cfg.Selectors, selDeps)
	if err != nil {
		return err
	}
	logger.Info("call selectors", "chain", cfg.FT8Ctrl.CallSelector)

	// A permissive ranker over the same deps backs the TUI "candidates in order"
	// view (only used in --tui mode).
	ranker := selector.NewRanker(selDeps)

	seq, err := sequencer.New(cfg.FT8Ctrl, chain, cmds, logger)
	if err != nil {
		return err
	}
	defer seq.Close()

	// Hot-reload the configuration on SIGHUP. Selector chain, blacklist,
	// retry_time, tx_retries, tx_power and follow_frequency take effect without
	// a restart; socket/database/identity fields require one (see warnImmutable).
	go reloadOnHUP(ctx, resolvedPath, &cfg, &members, seq, store, entities, &retryNanos, logger)

	if *tuiMode {
		ctrl := control.New(cfg, control.Deps{
			Store:      store,
			Members:    members,
			Continent:  ownContinent,
			Seq:        seq,
			RetryNanos: &retryNanos,
			Logger:     logger,
		})
		return runTUI(ctx, seq, store, ranker, ctrl, logSink, cfg.FT8Ctrl.MyCall, logger)
	}

	if err := seq.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

// runTUI runs the interactive front-end as the main loop while the sequencer
// drives WSJT-X on a background goroutine. Quitting the TUI cancels the daemon
// context so the sequencer, writer and purge goroutines stop; conversely a
// SIGINT (ctx cancellation) tears the TUI down.
func runTUI(ctx context.Context, seq *sequencer.Sequencer, store *db.Store, ranker *selector.Ranker, ctrl *control.Controller, logSink *applog.Sink, myCall string, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	seqErr := make(chan error, 1)
	go func() { seqErr <- seq.Run(ctx) }()

	err := tui.Run(ctx, tui.Deps{
		Store:   store,
		Seq:     seq,
		Ranker:  ranker,
		Control: ctrl,
		LogSink: logSink,
		MyCall:  myCall,
		Version: version,
	})
	cancel() // stop the sequencer now that the UI is gone

	if se := <-seqErr; se != nil && !errors.Is(se, context.Canceled) {
		logger.Error("sequencer stopped with error", "err", se)
	}
	logger.Info("shutdown complete")
	return err
}

// reloadOnHUP reloads the configuration file on each SIGHUP and applies the
// hot-reloadable settings without restarting the daemon. An invalid config (or
// a selector chain that fails to build) is logged and ignored: the running
// configuration is preserved. The pointers cfg and members are read/written
// only by this goroutine once the sequencer is running, so no locking is
// needed; retryNanos and the sequencer have their own synchronization.
func reloadOnHUP(ctx context.Context, path string, cfg **config.Config, members *selector.Membership,
	seq *sequencer.Sequencer, store *db.Store, entities *dxcc.DXCC, retryNanos *atomic.Int64, logger *slog.Logger) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
			newCfg, err := config.Load(path)
			if err != nil {
				logger.Error("reload failed, keeping current config", "path", path, "err", err)
				continue
			}
			warnImmutable(*cfg, newCfg, logger)

			// Load the LOTW list lazily if a newly-configured selector needs it
			// and it was not loaded at startup.
			mem := *members
			if mem == nil && needsLOTW(newCfg) {
				if cache, err := lotw.Default(); err != nil {
					logger.Warn("reload: LOTW unavailable, lotw_users_only selectors will accept everyone", "err", err)
				} else {
					mem = cache
				}
			}

			newChain, err := selector.Build(newCfg.FT8Ctrl.CallSelector, newCfg.Selectors, selector.Deps{
				Store:     store,
				Blacklist: blacklist.New(newCfg.BlackList),
				LOTW:      mem,
				Continent: operatorContinent(newCfg, entities, logger),
				Log:       logger,
			})
			if err != nil {
				logger.Error("reload: rebuilding selectors failed, keeping current config", "err", err)
				continue
			}

			seq.Reload(newCfg.FT8Ctrl, newChain)
			retryNanos.Store(int64(time.Duration(newCfg.FT8Ctrl.RetryTime) * time.Minute))
			*members = mem
			*cfg = newCfg
			logger.Info("configuration reloaded", "path", path, "chain", newCfg.FT8Ctrl.CallSelector)
		}
	}
}

// warnImmutable logs a warning for each field that changed in the new config but
// cannot be applied to a running daemon (it requires a restart): the sockets,
// the database and the station identity.
func warnImmutable(old, new *config.Config, logger *slog.Logger) {
	o, n := old.FT8Ctrl, new.FT8Ctrl
	warn := func(field, was, now string) {
		if was != now {
			logger.Warn("reload: change requires a restart, ignoring", "field", field, "was", was, "now", now)
		}
	}
	warn("my_call", o.MyCall, n.MyCall)
	warn("my_grid", o.MyGrid, n.MyGrid)
	warn("db_name", o.DBName, n.DBName)
	warn("wsjt_ip", o.WSJTIP, n.WSJTIP)
	warn("wsjt_port", fmt.Sprintf("%d", o.WSJTPort), fmt.Sprintf("%d", n.WSJTPort))
	warn("logger_ip", o.LoggerIP, n.LoggerIP)
	warn("logger_port", fmt.Sprintf("%d", o.LoggerPort), fmt.Sprintf("%d", n.LoggerPort))
	warn("logfile_name", o.LogfileName, n.LogfileName)
}

// operatorContinent returns the operator's own continent for the "ignore DX
// calling own continent" filter: the configured ft8ctrl.my_continent, or the
// continent of my_call resolved via DXCC, or "" when neither is available (the
// selectors then fall back to their built-in default).
func operatorContinent(cfg *config.Config, entities *dxcc.DXCC, logger *slog.Logger) string {
	if c := cfg.FT8Ctrl.MyContinent; c != "" {
		return c
	}
	if ent, err := entities.Lookup(cfg.FT8Ctrl.MyCall); err == nil && ent.Continent != "" {
		return ent.Continent
	}
	logger.Warn("could not determine own continent from my_call; using selector default",
		"my_call", cfg.FT8Ctrl.MyCall)
	return ""
}

// needsLOTW reports whether any configured selector restricts to LOTW users.
func needsLOTW(cfg *config.Config) bool {
	for _, name := range cfg.FT8Ctrl.CallSelector {
		if cfg.Selectors[name].LOTWUsersOnly {
			return true
		}
	}
	return false
}

// runPurge periodically removes stale unworked spots, like the Purge thread in
// dbutils.py. retryNanos holds the current retry_time (in nanoseconds) and may
// be updated live by a SIGHUP reload.
func runPurge(ctx context.Context, store *db.Store, retryNanos *atomic.Int64, logger *slog.Logger) {
	logger.Info("purge thread started", "retry", time.Duration(retryNanos.Load()))
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.Purge(time.Duration(retryNanos.Load()))
			if err != nil {
				logger.Error("purge", "err", err)
				continue
			}
			logger.Debug("purged stale rows", "count", n)
		}
	}
}
