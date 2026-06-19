package control

import (
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rampamac/ft8copilot/internal/config"
	"github.com/rampamac/ft8copilot/internal/db"
	"github.com/rampamac/ft8copilot/internal/sequencer"
)

func testDeps(t *testing.T) (Deps, *atomic.Int64) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "c.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cmds := make(chan db.Command, 1)
	seq, err := sequencer.New(
		config.FT8Ctrl{WSJTIP: "127.0.0.1", WSJTPort: 0, TXRetries: 5},
		nil, cmds, slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("sequencer.New: %v", err)
	}
	t.Cleanup(func() { seq.Close() })

	var retry atomic.Int64
	return Deps{
		Store:      store,
		Continent:  "EU",
		Seq:        seq,
		RetryNanos: &retry,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, &retry
}

func baseConfig() *config.Config {
	return &config.Config{
		FT8Ctrl: config.FT8Ctrl{
			TXPower: 30, TXRetries: 5, RetryTime: 15,
			CallSelector: config.StringList{"Any"},
		},
		BlackList: []string{"W5JDC"},
		Selectors: map[string]config.SelectorConfig{"Any": {}},
	}
}

func TestApplyUpdatesParamsAndRetry(t *testing.T) {
	deps, retry := testDeps(t)
	c := New(baseConfig(), deps)

	err := c.Apply(Params{
		TXPower: 20, TXRetries: 3, FollowFrequency: true, RetryTime: 10,
		CallSelector: []string{"Any"}, BlackList: []string{"k1abc", " "},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := c.Params()
	if got.TXPower != 20 || got.TXRetries != 3 || !got.FollowFrequency || got.RetryTime != 10 {
		t.Errorf("params not applied: %+v", got)
	}
	if len(got.BlackList) != 1 || got.BlackList[0] != "K1ABC" {
		t.Errorf("blacklist not normalized: %v", got.BlackList)
	}
	if want := int64(10 * time.Minute); retry.Load() != want {
		t.Errorf("retryNanos = %d, want %d", retry.Load(), want)
	}
}

func TestApplyRejectsUnknownSelector(t *testing.T) {
	deps, _ := testDeps(t)
	c := New(baseConfig(), deps)

	err := c.Apply(Params{
		TXPower: 99, CallSelector: []string{"DoesNotExist"},
	})
	if err == nil {
		t.Fatal("expected error for unknown selector")
	}
	// Nothing must have changed on failure.
	if got := c.Params(); got.TXPower != 30 {
		t.Errorf("config mutated on failed Apply: tx_power = %d, want 30", got.TXPower)
	}
}

func TestParamsSnapshotIsCopy(t *testing.T) {
	deps, _ := testDeps(t)
	c := New(baseConfig(), deps)
	p := c.Params()
	p.CallSelector[0] = "MUTATED"
	p.BlackList[0] = "MUTATED"
	if got := c.Params(); got.CallSelector[0] == "MUTATED" || got.BlackList[0] == "MUTATED" {
		t.Error("Params() returned slices aliasing internal state")
	}
}
