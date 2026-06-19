package db

import (
	"testing"
	"time"
)

func TestBandMetersFromName(t *testing.T) {
	cases := map[string]int{
		"20m": 20, "160m": 160, "2m": 2, "6m": 6,
		"70cm": 0, "": 0, "junk": 0, " 40M ": 40,
	}
	for name, want := range cases {
		if got := BandMetersFromName(name); got != want {
			t.Errorf("BandMetersFromName(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestImportWorkedSetsStatus(t *testing.T) {
	w, store := newTestWriter(t)
	when := time.Now().UTC()

	ok, err := w.ImportWorked(WorkedQSO{Call: "CO8LY", Band: 20, Grid: "FL11", Time: when})
	if err != nil || !ok {
		t.Fatalf("ImportWorked: ok=%v err=%v", ok, err)
	}
	rec, found, err := store.GetCall("CO8LY")
	if err != nil || !found {
		t.Fatalf("GetCall: found=%v err=%v", found, err)
	}
	if rec.Status != 2 {
		t.Errorf("status = %d, want 2 (worked)", rec.Status)
	}
	if rec.Country == "" {
		t.Errorf("country should be DXCC-enriched from the call, got empty")
	}
	if rec.Distance == 0 {
		t.Errorf("distance should be computed from the grid")
	}
}

func TestImportWorkedMissingGrid(t *testing.T) {
	w, store := newTestWriter(t)
	ok, err := w.ImportWorked(WorkedQSO{Call: "W6BSD", Band: 20, Time: time.Now().UTC()})
	if err != nil || !ok {
		t.Fatalf("ImportWorked (no grid): ok=%v err=%v", ok, err)
	}
	rec, found, _ := store.GetCall("W6BSD")
	if !found || rec.Status != 2 {
		t.Fatalf("record not stored as worked: found=%v status=%d", found, rec.Status)
	}
	if rec.Distance != 0 {
		t.Errorf("distance should be 0 without a grid, got %v", rec.Distance)
	}
	if rec.Country == "" {
		t.Errorf("country should still be DXCC-enriched, got empty")
	}
}

func TestImportWorkedIdempotent(t *testing.T) {
	w, store := newTestWriter(t)
	q := WorkedQSO{Call: "CO8LY", Band: 20, Grid: "FL11", Time: time.Now().UTC()}
	for i := 0; i < 3; i++ {
		if _, err := w.ImportWorked(q); err != nil {
			t.Fatalf("ImportWorked #%d: %v", i, err)
		}
	}
	rows, err := store.All(nil)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("re-import created %d rows, want 1 (idempotent)", len(rows))
	}
}

func TestImportWorkedUpgradesExistingSpot(t *testing.T) {
	w, store := newTestWriter(t)
	// A live, unworked spot first.
	if err := w.Process(InsertCmd{Spot: sampleSpot("CO8LY", -10, time.Now().UTC())}); err != nil {
		t.Fatalf("insert spot: %v", err)
	}
	if rec, _, _ := store.GetCall("CO8LY"); rec.Status != 0 {
		t.Fatalf("precondition: status = %d, want 0", rec.Status)
	}
	// Importing the same call/band upgrades it to worked.
	if _, err := w.ImportWorked(WorkedQSO{Call: "CO8LY", Band: 20, Time: time.Now().UTC()}); err != nil {
		t.Fatalf("ImportWorked: %v", err)
	}
	if rec, _, _ := store.GetCall("CO8LY"); rec.Status != 2 {
		t.Errorf("status after import = %d, want 2", rec.Status)
	}
}

func TestImportWorkedSeenByWorkedCountries(t *testing.T) {
	w, store := newTestWriter(t)
	when := time.Now().UTC()
	for _, c := range []string{"CO8LY", "CO2AAA"} { // both Cuba
		if _, err := w.ImportWorked(WorkedQSO{Call: c, Band: 20, Grid: "FL11", Time: when}); err != nil {
			t.Fatalf("import %s: %v", c, err)
		}
	}
	worked, err := store.WorkedCountries(20, 2)
	if err != nil {
		t.Fatalf("WorkedCountries: %v", err)
	}
	found := false
	for _, country := range worked {
		if country == "Cuba" {
			found = true
		}
	}
	if !found {
		t.Errorf("imported QSOs not seen by DXCC100/WorkedCountries: %v", worked)
	}
}

func TestImportWorkedFallbackCountry(t *testing.T) {
	w, store := newTestWriter(t)
	// A call DXCC can't resolve, but with an ADIF country provided.
	ok, err := w.ImportWorked(WorkedQSO{
		Call: "1X9ZZZ", Band: 20, Time: time.Now().UTC(),
		Country: "Narnia", Continent: "EU", CQZone: 14, ITUZone: 28,
	})
	if err != nil {
		t.Fatalf("ImportWorked: %v", err)
	}
	if !ok {
		t.Skip("DXCC resolved the synthetic call; fallback path not exercised")
	}
	if rec, found, _ := store.GetCall("1X9ZZZ"); !found || rec.Country != "Narnia" {
		t.Errorf("fallback country not stored: found=%v country=%q", found, rec.Country)
	}
}

func TestImportWorkedSkipsUnknownBand(t *testing.T) {
	w, _ := newTestWriter(t)
	ok, err := w.ImportWorked(WorkedQSO{Call: "CO8LY", Band: 0, Time: time.Now().UTC()})
	if err != nil {
		t.Fatalf("ImportWorked: %v", err)
	}
	if ok {
		t.Error("import with band 0 should be skipped")
	}
}
