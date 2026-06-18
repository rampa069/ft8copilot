package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rampamac/ft8copilot/internal/dxcc"
)

func TestBand(t *testing.T) {
	cases := map[uint64]int{
		14074000: 20,
		7074000:  40,
		1840000:  160,
		50313000: 6,
		28074000: 10,
		999000:   0, // below any band
		24915000: 12,
	}
	for freq, want := range cases {
		if got := Band(freq); got != want {
			t.Errorf("Band(%d) = %d, want %d", freq, got, want)
		}
	}
}

// newTestWriter returns a Writer backed by a fresh temp database plus the Store
// for direct queries.
func newTestWriter(t *testing.T) (*Writer, *Store) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	entities, err := dxcc.New()
	if err != nil {
		t.Fatalf("dxcc.New: %v", err)
	}
	w, err := NewWriter(store, "FM18", entities, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	return w, store
}

func sampleSpot(call string, snr int32, at time.Time) Spot {
	return Spot{
		Call:      call,
		Extra:     "",
		Grid:      "FL20",
		Frequency: 14074000,
		Band:      20,
		Packet: Packet{
			Time:    at,
			SNR:     snr,
			Mode:    "~",
			Message: "CQ " + call + " FL20",
		},
	}
}

func TestInsertAndGetCall(t *testing.T) {
	w, store := newTestWriter(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := w.Process(InsertCmd{Spot: sampleSpot("CO8LY", -7, now)}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rec, ok, err := store.GetCall("CO8LY")
	if err != nil || !ok {
		t.Fatalf("GetCall: ok=%v err=%v", ok, err)
	}
	if rec.Country != "Cuba" {
		t.Errorf("Country = %q, want Cuba", rec.Country)
	}
	if rec.Continent != "NA" {
		t.Errorf("Continent = %q, want NA", rec.Continent)
	}
	if rec.Status != 0 {
		t.Errorf("Status = %d, want 0", rec.Status)
	}
	if rec.SNR != -7 {
		t.Errorf("SNR = %d, want -7", rec.SNR)
	}
	if rec.Distance <= 0 {
		t.Errorf("Distance = %v, want > 0", rec.Distance)
	}
	if rec.Band != 20 {
		t.Errorf("Band = %d, want 20", rec.Band)
	}
	if rec.Packet.Message == "" || rec.Packet.Mode != "~" {
		t.Errorf("packet not round-tripped: %+v", rec.Packet)
	}
}

func TestUpsertUpdatesUntilWorked(t *testing.T) {
	w, store := newTestWriter(t)
	now := time.Now().UTC().Truncate(time.Second)

	// First insert.
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", -10, now)})
	// Second decode with a better SNR updates the row (status still 0).
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", -3, now)})
	rec, _, _ := store.GetCall("CO8LY")
	if rec.SNR != -3 {
		t.Fatalf("SNR after upsert = %d, want -3", rec.SNR)
	}

	// Mark as worked, then a later decode must NOT overwrite it.
	mustProcess(t, w, StatusCmd{Call: "CO8LY", Band: 20, Status: 2})
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", 5, now)})
	rec, _, _ = store.GetCall("CO8LY")
	if rec.SNR != -3 {
		t.Errorf("SNR after worked = %d, want unchanged -3", rec.SNR)
	}
	if rec.Status != 2 {
		t.Errorf("Status = %d, want 2", rec.Status)
	}
}

func TestStatusUpdateStopsAtWorked(t *testing.T) {
	w, store := newTestWriter(t)
	now := time.Now().UTC().Truncate(time.Second)
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", -7, now)})

	mustProcess(t, w, StatusCmd{Call: "CO8LY", Band: 20, Status: 1})
	if rec, _, _ := store.GetCall("CO8LY"); rec.Status != 1 {
		t.Fatalf("Status = %d, want 1", rec.Status)
	}
	mustProcess(t, w, StatusCmd{Call: "CO8LY", Band: 20, Status: 2})
	if rec, _, _ := store.GetCall("CO8LY"); rec.Status != 2 {
		t.Fatalf("Status = %d, want 2", rec.Status)
	}
	// status=2 is terminal: another update is a no-op (WHERE status <> 2).
	mustProcess(t, w, StatusCmd{Call: "CO8LY", Band: 20, Status: 1})
	if rec, _, _ := store.GetCall("CO8LY"); rec.Status != 2 {
		t.Errorf("Status = %d, want still 2", rec.Status)
	}
}

func TestDeleteInProgress(t *testing.T) {
	w, store := newTestWriter(t)
	now := time.Now().UTC().Truncate(time.Second)
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", -7, now)})

	// DeleteCmd only removes rows with status = 1.
	mustProcess(t, w, DeleteCmd{Call: "CO8LY", Band: 20})
	if _, ok, _ := store.GetCall("CO8LY"); !ok {
		t.Fatal("row removed while status=0, want kept")
	}
	mustProcess(t, w, StatusCmd{Call: "CO8LY", Band: 20, Status: 1})
	mustProcess(t, w, DeleteCmd{Call: "CO8LY", Band: 20})
	if _, ok, _ := store.GetCall("CO8LY"); ok {
		t.Error("row kept after delete of status=1, want removed")
	}
}

func TestRecentAndWorkedCountries(t *testing.T) {
	w, store := newTestWriter(t)
	now := time.Now().UTC().Truncate(time.Second)

	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", -7, now)})
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("G3XYZ", -5, now)})

	recent, err := store.Recent(20, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("Recent returned %d rows, want 2", len(recent))
	}
	// A future cutoff excludes everything.
	if r, _ := store.Recent(20, now.Add(time.Minute)); len(r) != 0 {
		t.Errorf("Recent(future) = %d rows, want 0", len(r))
	}

	// Mark Cuba worked; it should show up in WorkedCountries and drop from Recent.
	mustProcess(t, w, StatusCmd{Call: "CO8LY", Band: 20, Status: 2})
	worked, err := store.WorkedCountries(20, 1)
	if err != nil {
		t.Fatalf("WorkedCountries: %v", err)
	}
	if len(worked) != 1 || worked[0] != "Cuba" {
		t.Errorf("WorkedCountries = %v, want [Cuba]", worked)
	}
	if r, _ := store.Recent(20, now.Add(-time.Minute)); len(r) != 1 {
		t.Errorf("Recent after worked = %d rows, want 1", len(r))
	}
}

func TestPurge(t *testing.T) {
	w, store := newTestWriter(t)
	old := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fresh := time.Now().UTC().Truncate(time.Second)

	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", -7, old)})
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("G3XYZ", -7, fresh)})

	n, err := store.Purge(15 * time.Minute)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n != 1 {
		t.Errorf("Purge removed %d rows, want 1", n)
	}
	if _, ok, _ := store.GetCall("CO8LY"); ok {
		t.Error("stale row survived purge")
	}
	if _, ok, _ := store.GetCall("G3XYZ"); !ok {
		t.Error("fresh row was purged")
	}
}

func TestDeleteCallBand(t *testing.T) {
	w, store := newTestWriter(t)
	now := time.Now().UTC().Truncate(time.Second)
	mustProcess(t, w, InsertCmd{Spot: sampleSpot("CO8LY", -7, now)})

	n, err := store.DeleteCallBand("CO8LY", 20)
	if err != nil {
		t.Fatalf("DeleteCallBand: %v", err)
	}
	if n != 1 {
		t.Errorf("removed %d, want 1", n)
	}
	if _, ok, _ := store.GetCall("CO8LY"); ok {
		t.Error("row still present after DeleteCallBand")
	}
}

func TestInsertFakeCallSkipped(t *testing.T) {
	w, store := newTestWriter(t)
	now := time.Now().UTC().Truncate(time.Second)
	// A callsign with no DXCC entity should be skipped without error.
	spot := sampleSpot("12345", -7, now)
	if err := w.Process(InsertCmd{Spot: spot}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if _, ok, _ := store.GetCall("12345"); ok {
		t.Error("fake callsign was stored, want skipped")
	}
}

func mustProcess(t *testing.T, w *Writer, cmd Command) {
	t.Helper()
	if err := w.Process(cmd); err != nil {
		t.Fatalf("Process(%T): %v", cmd, err)
	}
}
