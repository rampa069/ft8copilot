package lotw

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedNow returns a clock pinned to t.
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestParseActivity(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-DefaultLastSeen) // 270 days before now

	csv := strings.Join([]string{
		"K1ABC,2026-06-01,10:00:00", // recent -> keep
		"w2def,2026-05-15,08:30:00", // recent, lowercase -> keep, uppercased
		"N3OLD,2024-01-01,00:00:00", // older than cutoff -> drop
		"GARBAGE-LINE-NO-COMMAS",    // malformed -> skip
		"K4BAD,not-a-date,00:00:00", // bad date -> skip
		",2026-06-10,00:00:00",      // empty call -> skip
		"",                          // blank -> skip
		"K5XYZ,2026-06-17,23:59:59", // recent -> keep
	}, "\n")

	users, err := parseActivity(strings.NewReader(csv), cutoff)
	if err != nil {
		t.Fatalf("parseActivity: %v", err)
	}

	want := []string{"K1ABC", "W2DEF", "K5XYZ"}
	if len(users) != len(want) {
		t.Fatalf("got %d users %v, want %d", len(users), users, len(want))
	}
	for _, w := range want {
		if _, ok := users[w]; !ok {
			t.Errorf("expected %q in set", w)
		}
	}
	if _, ok := users["N3OLD"]; ok {
		t.Error("N3OLD older than cutoff should be dropped")
	}
	if _, ok := users["K4BAD"]; ok {
		t.Error("K4BAD with bad date should be skipped")
	}
}

func TestParseActivityCutoffBoundary(t *testing.T) {
	now := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	cutoff := now.Add(-10 * 24 * time.Hour) // 2026-06-08 00:00:00

	csv := strings.Join([]string{
		"ONCUT,2026-06-08,00:00:00", // exactly at cutoff -> keep (not Before)
		"BELOW,2026-06-07,23:00:00", // before cutoff -> drop
	}, "\n")

	users, err := parseActivity(strings.NewReader(csv), cutoff)
	if err != nil {
		t.Fatalf("parseActivity: %v", err)
	}
	if _, ok := users["ONCUT"]; !ok {
		t.Error("date equal to cutoff should be kept")
	}
	if _, ok := users["BELOW"]; ok {
		t.Error("date before cutoff should be dropped")
	}
}

func TestContainsCaseInsensitive(t *testing.T) {
	c := &Cache{users: map[string]struct{}{"K1ABC": {}}}

	for _, in := range []string{"K1ABC", "k1abc", "K1abc", "  k1abc  "} {
		if !c.Contains(in) {
			t.Errorf("Contains(%q) = false, want true", in)
		}
	}
	if c.Contains("NOPE") {
		t.Error("Contains(NOPE) = true, want false")
	}
}

// fakeFetch returns a reader over body and records that it was called.
func fakeFetch(body string, called *int) func(string) (io.ReadCloser, error) {
	return func(string) (io.ReadCloser, error) {
		*called++
		return io.NopCloser(strings.NewReader(body)), nil
	}
}

func TestRoundTripPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lotw_cache.dat")
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	csv := strings.Join([]string{
		"K1ABC,2026-06-01,10:00:00",
		"w2def,2026-06-02,10:00:00",
	}, "\n")

	var fetches int
	c, err := New(path,
		WithFetch(fakeFetch(csv, &fetches)),
		WithNow(fixedNow(now)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Missing file -> one rebuild.
	if fetches != 1 {
		t.Fatalf("expected 1 fetch on missing cache, got %d", fetches)
	}
	if !c.Contains("K1ABC") || !c.Contains("w2def") {
		t.Fatal("members missing after build")
	}

	// Reload with a fresh (non-expired) cache: no new fetch.
	fetches = 0
	c2, err := New(path,
		WithFetch(fakeFetch(csv, &fetches)),
		WithNow(fixedNow(now.Add(time.Hour))),
	)
	if err != nil {
		t.Fatalf("New reload: %v", err)
	}
	if fetches != 0 {
		t.Errorf("fresh cache should not refetch, got %d fetches", fetches)
	}
	if !c2.Contains("K1ABC") || !c2.Contains("W2DEF") {
		t.Error("membership not preserved across reload")
	}
}

func TestExpiredCacheTriggersRebuild(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lotw_cache.dat")
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	// First build writes a cache stamped at `now`.
	var fetches int
	if _, err := New(path,
		WithFetch(fakeFetch("OLDCALL,2026-06-01,10:00:00", &fetches)),
		WithNow(fixedNow(now)),
	); err != nil {
		t.Fatalf("initial New: %v", err)
	}

	// Reopen far in the future so the cache is expired; inject a new dataset.
	later := now.Add(DefaultExpire + time.Hour)
	fetches = 0
	newCSV := "NEWCALL,2026-06-15,10:00:00"
	c, err := New(path,
		WithFetch(fakeFetch(newCSV, &fetches)),
		WithNow(fixedNow(later)),
	)
	if err != nil {
		t.Fatalf("expired New: %v", err)
	}
	if fetches != 1 {
		t.Errorf("expired cache should refetch once, got %d", fetches)
	}
	if !c.Contains("NEWCALL") {
		t.Error("rebuilt cache should contain new data")
	}
	if c.Contains("OLDCALL") {
		t.Error("rebuilt cache should replace old data")
	}
}
