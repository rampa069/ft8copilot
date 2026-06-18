package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rampamac/ft8copilot/internal/db"
)

func TestRenderTable(t *testing.T) {
	recs := []db.Record{
		{
			Call:      "W6BSD",
			Status:    2,
			Band:      20,
			SNR:       -7,
			Grid:      "CM87",
			CQZone:    3,
			ITUZone:   6,
			Country:   "United States",
			Continent: "NA",
			Time:      time.Date(2026, 6, 18, 12, 34, 56, 0, time.UTC),
			Extra:     "",
		},
		{
			Call:      "G3XYZ",
			Status:    0,
			Band:      40,
			SNR:       3,
			Grid:      "IO91",
			CQZone:    14,
			ITUZone:   27,
			Country:   "England",
			Continent: "EU",
			Time:      time.Date(2026, 6, 18, 12, 35, 0, 0, time.UTC),
			Extra:     "DXCC100",
		},
	}

	var sb strings.Builder
	if err := renderTable(&sb, recs, lotwLookup{}); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := sb.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d:\n%s", len(lines), out)
	}

	// Header has every expected column.
	for _, col := range []string{"call", "status", "band", "snr", "grid", "cqzone",
		"ituzone", "country", "continent", "time", "extra", "lotw"} {
		if !strings.Contains(lines[0], col) {
			t.Errorf("header missing column %q: %s", col, lines[0])
		}
	}

	// Data fields present.
	if !strings.Contains(lines[1], "W6BSD") || !strings.Contains(lines[1], "United States") {
		t.Errorf("row 1 missing data: %s", lines[1])
	}
	if !strings.Contains(lines[1], "2026-06-18 12:34:56") {
		t.Errorf("row 1 missing formatted time: %s", lines[1])
	}
	if !strings.Contains(lines[2], "G3XYZ") || !strings.Contains(lines[2], "DXCC100") {
		t.Errorf("row 2 missing data: %s", lines[2])
	}

	// LOTW disabled => every row shows "-".
	for _, ln := range lines[1:] {
		fields := strings.Fields(ln)
		if last := fields[len(fields)-1]; last != "-" {
			t.Errorf("expected lotw '-' with disabled lookup, got %q in: %s", last, ln)
		}
	}
}

func TestRenderTableLOTW(t *testing.T) {
	// A nil cache reports non-member; we only verify the contains seam here.
	lk := lotwLookup{}
	if lk.contains("W6BSD") {
		t.Error("disabled lotwLookup should report non-member")
	}
}
