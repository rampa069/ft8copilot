package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/dxcc"
)

func seedStore(t *testing.T, calls ...string) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "search.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ent, err := dxcc.New()
	if err != nil {
		t.Fatalf("dxcc.New: %v", err)
	}
	w, err := db.NewWriter(store, "FM18", ent, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for _, c := range calls {
		spot := db.Spot{
			Call: c, Grid: "FL11", Frequency: 14074000, Band: 20,
			Packet: db.Packet{Time: time.Now().UTC(), SNR: -7, Mode: "~", Message: "CQ " + c + " FL11"},
		}
		if err := w.Process(db.InsertCmd{Spot: spot}); err != nil {
			t.Fatalf("insert %s: %v", c, err)
		}
	}
	return store
}

func TestSearchModalRun(t *testing.T) {
	store := seedStore(t, "CO8LY", "CO2AAA", "W6BSD")
	s := newSearchModal(store)
	s.input.SetValue("co")
	s.run()
	if len(s.results) != 2 {
		t.Fatalf("got %d results, want 2", len(s.results))
	}
	if !strings.Contains(s.status, "2 result") {
		t.Errorf("status = %q, want it to mention 2 results", s.status)
	}
}

func TestSearchModalNoStore(t *testing.T) {
	s := newSearchModal(nil)
	s.run()
	if s.status != "no database" {
		t.Errorf("status = %q, want 'no database'", s.status)
	}
}

func TestSearchModalScroll(t *testing.T) {
	s := newSearchModal(nil)
	s.results = make([]db.Record, 20)
	down := tea.KeyMsg{Type: tea.KeyDown}
	s, _ = s.update(down)
	s, _ = s.update(down)
	if s.offset != 2 {
		t.Errorf("offset after 2 downs = %d, want 2", s.offset)
	}
	up := tea.KeyMsg{Type: tea.KeyUp}
	s, _ = s.update(up)
	s, _ = s.update(up)
	s, _ = s.update(up) // clamps at 0
	if s.offset != 0 {
		t.Errorf("offset after clamping = %d, want 0", s.offset)
	}
}

func TestSearchModalTypes(t *testing.T) {
	s := newSearchModal(nil)
	s.focus() // input only accepts text when focused
	for _, r := range "co8" {
		s, _ = s.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if s.input.Value() != "co8" {
		t.Errorf("input value = %q, want co8", s.input.Value())
	}
}

func TestRenderRecordsLayout(t *testing.T) {
	recs := []db.Record{
		{Call: "CO8LY", Country: "Cuba", Grid: "FL11", SNR: -5, Band: 20, Status: 2},
		{Call: "W6BSD", Country: "United States", Grid: "CM87", SNR: -9, Band: 20, Status: 0},
	}
	out := renderRecords(recs, 56, 5, 0)
	if h := lipgloss.Height(out); h != 5 {
		t.Errorf("height = %d, want 5", h)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 56 {
			t.Errorf("line %d width = %d, want 56", i, w)
		}
	}
	p := plain(out)
	if !strings.Contains(p, "CALL") || !strings.Contains(p, "wkd") || !strings.Contains(p, "new") {
		t.Errorf("missing header/status labels:\n%s", p)
	}
}

func TestModelF3OpensAndEscCloses(t *testing.T) {
	m := newModel(Deps{})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF3})
	mm := updated.(model)
	if !mm.searching {
		t.Fatal("F3 should open the search modal")
	}
	_ = cmd
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).searching {
		t.Error("Esc should close the search modal")
	}
}

func TestStatusLabel(t *testing.T) {
	cases := map[int]string{0: "new", 1: "wip", 2: "wkd", 9: "?"}
	for st, want := range cases {
		if got := statusLabel(st); got != want {
			t.Errorf("statusLabel(%d) = %q, want %q", st, got, want)
		}
	}
}
