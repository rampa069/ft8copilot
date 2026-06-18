package blacklist

import "testing"

func TestContainsCaseInsensitive(t *testing.T) {
	b := New([]string{"w1aw", "K2ABC", "Vk3xyz"})

	cases := []struct {
		call string
		want bool
	}{
		{"W1AW", true},
		{"w1aw", true},
		{"  k2abc  ", true}, // trimmed and uppercased
		{"VK3XYZ", true},
		{"N0CALL", false}, // absent
		{"", false},
	}
	for _, tc := range cases {
		if got := b.Contains(tc.call); got != tc.want {
			t.Errorf("Contains(%q) = %v, want %v", tc.call, got, tc.want)
		}
	}
}

func TestNewTrimsAndUppercases(t *testing.T) {
	b := New([]string{"  w1aw  ", "", "   "})
	if b.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 (empty/whitespace entries dropped)", b.Len())
	}
	if !b.Contains("W1AW") {
		t.Errorf("expected W1AW to be present after trimming")
	}
}

func TestLen(t *testing.T) {
	b := New([]string{"a", "B", "c"})
	if got := b.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
}

func TestNilSafe(t *testing.T) {
	var b *Blacklist
	if b.Contains("W1AW") {
		t.Errorf("nil *Blacklist Contains should return false")
	}
	if b.Len() != 0 {
		t.Errorf("nil *Blacklist Len should return 0, got %d", b.Len())
	}
}

func TestEmpty(t *testing.T) {
	b := New(nil)
	if b.Contains("W1AW") {
		t.Errorf("empty Blacklist should not contain anything")
	}
	if b.Len() != 0 {
		t.Errorf("empty Blacklist Len should be 0, got %d", b.Len())
	}
}
