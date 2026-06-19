// Package tui is the interactive terminal front-end for ft8ctrl, rendered with
// the retro DOS / Turbo Vision aesthetic of BlueBEEP (blue background,
// double-line borders, a function-key menu bar). It runs in-process: the
// Sequencer keeps driving WSJT-X on its own goroutine while this package owns the
// terminal and presents live state.
//
// This file is the scaffold (FT8CoPilot-5km): a minimal Bubble Tea program that
// boots, renders a placeholder frame and shuts down cleanly. The rich layout,
// theme and views are built on top of it by the sibling TUI tasks.
package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rampamac/ft8copilot/internal/control"
	"github.com/rampamac/ft8copilot/internal/db"
	applog "github.com/rampamac/ft8copilot/internal/log"
	"github.com/rampamac/ft8copilot/internal/selector"
	"github.com/rampamac/ft8copilot/internal/sequencer"
)

// Deps are the live daemon components the TUI reads from and controls. The
// scaffold only needs a handful; later tasks (log fan-out, parameter control,
// candidate ranking) extend this struct.
type Deps struct {
	// Store is the cqcalls database, used by the search and candidate views.
	Store *db.Store
	// Seq is the running sequencer, used to pause/resume the autopilot and to
	// read live status.
	Seq *sequencer.Sequencer
	// Ranker ranks the band's candidate pool for the candidates view. May be nil.
	Ranker *selector.Ranker
	// Control applies live parameter changes from the editor. May be nil.
	Control *control.Controller
	// LogSink streams daemon log records to the log window. May be nil.
	LogSink *applog.Sink
	// MyCall is the operator's callsign, shown in the header.
	MyCall string
	// Version is shown in the header banner.
	Version string
}

// Run starts the TUI and blocks until the user quits or ctx is cancelled. It is
// the main-loop replacement for seq.Run when ft8ctrl is launched with --tui.
// Cancelling ctx (e.g. on SIGINT) tears the program down; conversely, quitting
// the program returns nil so the caller can cancel the daemon context.
func Run(ctx context.Context, d Deps) error {
	p := tea.NewProgram(newModel(d), tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil {
		// A context cancellation (SIGINT, daemon shutdown) is a clean exit, not
		// an error to surface.
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}
