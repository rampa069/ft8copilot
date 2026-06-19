package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rampamac/ft8copilot/internal/adif"
	"github.com/rampamac/ft8copilot/internal/db"
	"github.com/rampamac/ft8copilot/internal/dxcc"
)

func TestParseFreqHz(t *testing.T) {
	cases := map[string]uint64{
		"14.074": 14074000,
		"7.074":  7074000,
		"":       0,
		"junk":   0,
		"0":      0,
	}
	for s, want := range cases {
		if got := parseFreqHz(s); got != want {
			t.Errorf("parseFreqHz(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestParseQSOTime(t *testing.T) {
	if got := parseQSOTime("20210101", "1234"); got.Format("2006-01-02 15:04") != "2021-01-01 12:34" {
		t.Errorf("HHMM parse = %v", got)
	}
	if got := parseQSOTime("20210101", "123456"); got.Format("15:04:05") != "12:34:56" {
		t.Errorf("HHMMSS parse = %v", got)
	}
	if got := parseQSOTime("", "1234"); !got.IsZero() {
		t.Errorf("missing date should be zero time, got %v", got)
	}
}

func TestRecordToQSO(t *testing.T) {
	recs, err := adif.Parse(strings.NewReader(
		"<call:5>co8ly<band:3>20m<gridsquare:4>FL11<eor>" +
			"<call:5>W6BSD<freq:6>14.074<eor>" + // band via freq fallback
			"<band:3>40m<eor>" + // no call
			"<call:6>NOBAND<eor>", // no band
	))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	q0, r0 := recordToQSO(recs[0])
	if r0 != "" || q0.Call != "CO8LY" || q0.Band != 20 || q0.Grid != "FL11" {
		t.Errorf("rec0 = %+v reason=%q", q0, r0)
	}
	q1, r1 := recordToQSO(recs[1])
	if r1 != "" || q1.Band != 20 { // 14.074 MHz -> 20m
		t.Errorf("rec1 band via freq = %d reason=%q", q1.Band, r1)
	}
	if _, r2 := recordToQSO(recs[2]); r2 != "no call" {
		t.Errorf("rec2 reason = %q, want 'no call'", r2)
	}
	if _, r3 := recordToQSO(recs[3]); r3 != "no band" {
		t.Errorf("rec3 reason = %q, want 'no band'", r3)
	}
}

func TestFormatBands(t *testing.T) {
	got := formatBands(map[int]int{20: 4507, 40: 885, 6: 67})
	if got != "40m=885  20m=4507  6m=67" {
		t.Errorf("formatBands = %q", got)
	}
}

// End-to-end: parse a small ADIF and import it into a temp store, asserting the
// rows land as worked.
func TestImportEndToEnd(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "imp.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	entities, err := dxcc.New()
	if err != nil {
		t.Fatalf("dxcc.New: %v", err)
	}
	writer, err := db.NewWriter(store, "FM18", entities, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	recs, _ := adif.Parse(strings.NewReader(
		"<call:5>CO8LY<band:3>20m<gridsquare:4>FL11<qso_date:8>20210101<time_on:4>1200<eor>" +
			"<call:5>W6BSD<freq:6>14.074<eor>",
	))
	imported := 0
	for _, rec := range recs {
		q, reason := recordToQSO(rec)
		if reason != "" {
			continue
		}
		ok, err := writer.ImportWorked(q)
		if err != nil {
			t.Fatalf("ImportWorked: %v", err)
		}
		if ok {
			imported++
		}
	}
	if imported != 2 {
		t.Fatalf("imported %d, want 2", imported)
	}
	rec, found, _ := store.GetCall("CO8LY")
	if !found || rec.Status != 2 || rec.Band != 20 {
		t.Errorf("CO8LY = %+v found=%v, want worked on 20m", rec, found)
	}
}
