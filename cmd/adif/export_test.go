package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rampamac/ft8copilot/internal/adif"
	"github.com/rampamac/ft8copilot/internal/db"
	"github.com/rampamac/ft8copilot/internal/dxcc"
)

func TestRecordToADIF(t *testing.T) {
	r := db.Record{
		Call: "CO8LY", Band: 20, Grid: "FL11", Country: "Cuba",
		CQZone: 8, ITUZone: 11, Frequency: 14074000,
		Time: time.Date(2021, 1, 2, 12, 34, 56, 0, time.UTC),
	}
	rec := recordToADIF(r, "EA5IUE", "IM98NK")

	want := map[string]string{
		"CALL": "CO8LY", "BAND": "20m", "MODE": "FT8",
		"QSO_DATE": "20210102", "TIME_ON": "123456",
		"FREQ": "14.074000", "GRIDSQUARE": "FL11", "COUNTRY": "Cuba",
		"CQZ": "8", "ITUZ": "11",
		"STATION_CALLSIGN": "EA5IUE", "MY_GRIDSQUARE": "IM98NK",
	}
	for k, v := range want {
		if got, _ := rec.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestRecordToADIFOmitsEmpties(t *testing.T) {
	rec := recordToADIF(db.Record{Call: "K1ABC", Band: 40}, "", "")
	for _, absent := range []string{"FREQ", "GRIDSQUARE", "COUNTRY", "QSO_DATE", "STATION_CALLSIGN"} {
		if _, ok := rec.Get(absent); ok {
			t.Errorf("%s should be omitted when unset", absent)
		}
	}
}

// End-to-end: seed a store, export the worked rows, re-parse the ADIF.
func TestWriteADIFRoundTrip(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "exp.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	entities, _ := dxcc.New()
	writer, _ := db.NewWriter(store, "FM18", entities, nil)

	// One worked QSO and one live (unworked) spot.
	if _, err := writer.ImportWorked(db.WorkedQSO{Call: "CO8LY", Band: 20, Grid: "FL11", Time: time.Now().UTC()}); err != nil {
		t.Fatalf("ImportWorked: %v", err)
	}
	if err := writer.Process(db.InsertCmd{Spot: db.Spot{
		Call: "W6BSD", Grid: "CM87", Frequency: 14074000, Band: 20,
		Packet: db.Packet{Time: time.Now().UTC(), Mode: "~", Message: "CQ W6BSD CM87"},
	}}); err != nil {
		t.Fatalf("insert spot: %v", err)
	}

	// Export only worked rows.
	worked := 2
	rows, err := store.Search(db.Query{Status: &worked})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var b strings.Builder
	if err := writeADIF(&b, rows, "EA5IUE", "IM98NK"); err != nil {
		t.Fatalf("writeADIF: %v", err)
	}

	recs, err := adif.Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("exported %d records, want 1 (only worked)", len(recs))
	}
	if v, _ := recs[0].Get("CALL"); v != "CO8LY" {
		t.Errorf("exported call = %q, want CO8LY", v)
	}
	if v, _ := recs[0].Get("BAND"); v != "20m" {
		t.Errorf("exported band = %q, want 20m", v)
	}
	if v, _ := recs[0].Get("STATION_CALLSIGN"); v != "EA5IUE" {
		t.Errorf("station_callsign = %q, want EA5IUE", v)
	}
}
