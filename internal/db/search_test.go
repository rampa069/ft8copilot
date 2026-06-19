package db

import (
	"testing"
	"time"
)

func intPtr(n int) *int { return &n }

// seedSearch inserts a small varied dataset and returns the writer/store.
func seedSearch(t *testing.T) *Store {
	t.Helper()
	w, store := newTestWriter(t)
	base := time.Now().UTC()
	rows := []struct {
		call string
		snr  int32
		at   time.Time
	}{
		{"CO8LY", -5, base.Add(-3 * time.Minute)}, // Cuba
		{"W6BSD", -7, base.Add(-2 * time.Minute)}, // United States
		{"G3XYZ", -9, base.Add(-1 * time.Minute)}, // England
		{"CO2AAA", -4, base},                      // Cuba (most recent)
	}
	for _, r := range rows {
		if err := w.Process(InsertCmd{Spot: sampleSpot(r.call, r.snr, r.at)}); err != nil {
			t.Fatalf("insert %s: %v", r.call, err)
		}
	}
	return store
}

func TestSearchByCallSubstring(t *testing.T) {
	store := seedSearch(t)
	got, err := store.Search(Query{Text: "co"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// CO8LY and CO2AAA, most recent first.
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), calls(got))
	}
	if got[0].Call != "CO2AAA" || got[1].Call != "CO8LY" {
		t.Errorf("order = %v, want [CO2AAA CO8LY] (recent first)", calls(got))
	}
}

func TestSearchByCountry(t *testing.T) {
	store := seedSearch(t)
	got, err := store.Search(Query{Text: "United States"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Call != "W6BSD" {
		t.Errorf("got %v, want [W6BSD]", calls(got))
	}
}

func TestSearchBandAndStatusFilter(t *testing.T) {
	store := seedSearch(t)
	got, err := store.Search(Query{Band: intPtr(20), Status: intPtr(0)})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("band+status filter got %d rows, want 4", len(got))
	}
	if none, _ := store.Search(Query{Band: intPtr(40)}); len(none) != 0 {
		t.Errorf("band 40 should be empty, got %d", len(none))
	}
}

func TestSearchLimit(t *testing.T) {
	store := seedSearch(t)
	got, err := store.Search(Query{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("limit 2 got %d rows", len(got))
	}
}

func TestSearchEmptyMatchesAll(t *testing.T) {
	store := seedSearch(t)
	got, err := store.Search(Query{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("empty query got %d rows, want all 4", len(got))
	}
}

func calls(recs []Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Call
	}
	return out
}
