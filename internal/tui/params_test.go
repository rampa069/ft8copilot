package tui

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/control"
	"github.com/rampa069/ft8copilot/internal/db"
)

func testController(t *testing.T) *control.Controller {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "pc.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{
		FT8Ctrl: config.FT8Ctrl{
			TXPower: 30, TXRetries: 5, RetryTime: 15,
			CallSelector: config.StringList{"Any"},
		},
		BlackList: []string{"W5JDC"},
		Selectors: map[string]config.SelectorConfig{"Any": {}},
	}
	return control.New(cfg, control.Deps{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestParamModalOpenLoadsValues(t *testing.T) {
	m := newParamModal(testController(t))
	m.open()
	if got := m.inputs[fldTXPower].Value(); got != "30" {
		t.Errorf("tx power field = %q, want 30", got)
	}
	if got := m.inputs[fldSelectors].Value(); got != "Any" {
		t.Errorf("selectors field = %q, want Any", got)
	}
	if got := m.inputs[fldFollowFreq].Value(); got != "no" {
		t.Errorf("follow freq field = %q, want no", got)
	}
}

func TestParamModalApplyLive(t *testing.T) {
	ctrl := testController(t)
	m := newParamModal(ctrl)
	m.open()
	m.inputs[fldTXPower].SetValue("18")
	m.inputs[fldFollowFreq].SetValue("yes")
	m.apply()
	if !strings.Contains(m.status, "applied") {
		t.Fatalf("status = %q, want applied", m.status)
	}
	p := ctrl.Params()
	if p.TXPower != 18 || !p.FollowFrequency {
		t.Errorf("controller not updated: %+v", p)
	}
}

func TestParamModalInvalidNumber(t *testing.T) {
	m := newParamModal(testController(t))
	m.open()
	m.inputs[fldTXPower].SetValue("notanumber")
	m.apply()
	if !strings.Contains(m.status, "TX power") {
		t.Errorf("status = %q, want a TX power error", m.status)
	}
}

func TestParamModalUnknownSelectorError(t *testing.T) {
	m := newParamModal(testController(t))
	m.open()
	m.inputs[fldSelectors].SetValue("Nope")
	m.apply()
	if !strings.Contains(m.status, "error:") {
		t.Errorf("status = %q, want an apply error", m.status)
	}
}

func TestParamModalNavigation(t *testing.T) {
	m := newParamModal(testController(t))
	m.open()
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyDown})
	if m.focus != 1 {
		t.Errorf("focus after down = %d, want 1", m.focus)
	}
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyUp})
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyUp}) // wraps to last
	if m.focus != numFields-1 {
		t.Errorf("focus after wrap = %d, want %d", m.focus, numFields-1)
	}
}

func TestModelF4OpensParams(t *testing.T) {
	m := newModel(Deps{Control: testController(t)})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF4})
	if !updated.(model).editing {
		t.Fatal("F4 should open the parameter editor")
	}
	closed, _ := updated.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if closed.(model).editing {
		t.Error("Esc should close the parameter editor")
	}
}

func TestParseBool(t *testing.T) {
	for _, s := range []string{"yes", "Y", "true", "on", "1"} {
		if b, err := parseBool(s); err != nil || !b {
			t.Errorf("parseBool(%q) = %v,%v want true,nil", s, b, err)
		}
	}
	for _, s := range []string{"no", "N", "false", "off", "0"} {
		if b, err := parseBool(s); err != nil || b {
			t.Errorf("parseBool(%q) = %v,%v want false,nil", s, b, err)
		}
	}
	if _, err := parseBool("maybe"); err == nil {
		t.Error("parseBool(maybe) should error")
	}
}
