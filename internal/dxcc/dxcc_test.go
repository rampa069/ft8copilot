package dxcc

import (
	"errors"
	"sort"
	"testing"
)

func newTest(t *testing.T) *DXCC {
	t.Helper()
	d, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if d == nil {
		t.Fatal("New() returned nil DXCC")
	}
	return d
}

func TestLookup(t *testing.T) {
	d := newTest(t)

	cases := []struct {
		call      string
		country   string
		continent string
	}{
		{"W6BSD", "United States", "NA"},
		{"G3XYZ", "England", "EU"},
		{"JA1XYZ", "Japan", "AS"},
		{"VK2ABC", "Australia", "OC"},
		{"CO8LY", "Cuba", "NA"},
		{"DL1ABC", "Fed. Rep. of Germany", "EU"},
		{"VE3XYZ", "Canada", "NA"},
		{"w6bsd", "United States", "NA"}, // lowercase input is normalised
	}

	for _, c := range cases {
		got, err := d.Lookup(c.call)
		if err != nil {
			t.Errorf("Lookup(%q) error: %v", c.call, err)
			continue
		}
		if got.Country != c.country {
			t.Errorf("Lookup(%q).Country = %q, want %q", c.call, got.Country, c.country)
		}
		if got.Continent != c.continent {
			t.Errorf("Lookup(%q).Continent = %q, want %q", c.call, got.Continent, c.continent)
		}
		if got.CQZone <= 0 || got.ITUZone <= 0 {
			t.Errorf("Lookup(%q) zones = CQ %d / ITU %d, want positive", c.call, got.CQZone, got.ITUZone)
		}
	}
}

func TestLookupInvalid(t *testing.T) {
	d := newTest(t)
	for _, call := range []string{"", "12345!!!", "QZ9999", "00000"} {
		if _, err := d.Lookup(call); !errors.Is(err, ErrNotFound) {
			t.Errorf("Lookup(%q) err = %v, want ErrNotFound", call, err)
		}
	}
}

func TestEntities(t *testing.T) {
	d := newTest(t)
	ents := d.Entities()
	if len(ents) == 0 {
		t.Fatal("Entities() returned empty slice")
	}
	if !sort.StringsAreSorted(ents) {
		t.Error("Entities() is not sorted")
	}
}

func TestIsEntity(t *testing.T) {
	d := newTest(t)
	if !d.IsEntity("United States") {
		t.Error(`IsEntity("United States") = false, want true`)
	}
	if d.IsEntity("Notarealcountry") {
		t.Error(`IsEntity("Notarealcountry") = true, want false`)
	}
}

func TestGetEntity(t *testing.T) {
	d := newTest(t)
	prefixes, err := d.GetEntity("Cuba")
	if err != nil {
		t.Fatalf("GetEntity(\"Cuba\") error: %v", err)
	}
	if len(prefixes) == 0 {
		t.Fatal("GetEntity(\"Cuba\") returned no prefixes")
	}
	if !sort.StringsAreSorted(prefixes) {
		t.Error("GetEntity prefixes not sorted")
	}
	found := false
	for _, p := range prefixes {
		if p == "CM" || p == "CO" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetEntity(\"Cuba\") = %v, expected to contain a Cuba prefix", prefixes)
	}

	if _, err := d.GetEntity("Notarealcountry"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEntity(unknown) err = %v, want ErrNotFound", err)
	}
}

func TestExactMatchOverride(t *testing.T) {
	// =N2NL/MM is an exact-call US entry; ensure exact matching beats prefix.
	d := newTest(t)
	got, err := d.Lookup("N2NL/MM")
	if err != nil {
		t.Fatalf("Lookup(N2NL/MM) error: %v", err)
	}
	if got.Country != "United States" {
		t.Errorf("Lookup(N2NL/MM).Country = %q, want United States", got.Country)
	}
}
