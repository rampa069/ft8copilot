package db

import "testing"

// TestBandTableParity covers every entry of the Python get_band map in
// dbutils.py at a representative in-band frequency, plus out-of-band cases. The
// Python semantics are int(freq / 1e6) -> band lookup, default 0.
func TestBandTableParity(t *testing.T) {
	// One representative dial frequency per band, exercising every map key.
	inBand := []struct {
		freqHz uint64
		band   int
	}{
		{1840000, 160}, // 1 MHz  -> 160 m  (FT8 on 160)
		{3573000, 80},  // 3 MHz  -> 80 m
		{7074000, 40},  // 7 MHz  -> 40 m
		{10136000, 30}, // 10 MHz -> 30 m
		{14074000, 20}, // 14 MHz -> 20 m
		{18100000, 17}, // 18 MHz -> 17 m
		{21074000, 15}, // 21 MHz -> 15 m
		{24915000, 12}, // 24 MHz -> 12 m
		{28074000, 10}, // 28 MHz -> 10 m
		{50313000, 6},  // 50 MHz -> 6 m
	}
	for _, tc := range inBand {
		if got := Band(tc.freqHz); got != tc.band {
			t.Errorf("Band(%d) = %d, want %d", tc.freqHz, got, tc.band)
		}
		// The lookup keys on the integer MHz part, so any frequency within the
		// same MHz maps identically (e.g. the bottom of the MHz).
		mhz := tc.freqHz / 1_000_000
		if got := Band(mhz * 1_000_000); got != tc.band {
			t.Errorf("Band(%d MHz) = %d, want %d", mhz, got, tc.band)
		}
	}

	// Out-of-band and gap frequencies all return 0 (Python: key not in map).
	outOfBand := []uint64{
		0,           // DC
		999_000,     // 0 MHz, below 160 m
		2_000_000,   // 2 MHz, between 160 and 80
		5_000_000,   // 5 MHz, 60 m gap (not in the map)
		12_000_000,  // 12 MHz gap
		29_000_000,  // 29 MHz, above 28 (28 m key only)
		144_000_000, // 144 MHz, 2 m (not supported)
	}
	for _, freq := range outOfBand {
		if got := Band(freq); got != 0 {
			t.Errorf("Band(%d) = %d, want 0 (out of band)", freq, got)
		}
	}

	// The map is keyed on truncated MHz, so 28.999 MHz still resolves to 10 m
	// but 29.0 MHz does not (mirrors int(freq/1e6)).
	if got := Band(28_999_999); got != 10 {
		t.Errorf("Band(28.999 MHz) = %d, want 10", got)
	}
	if got := Band(29_000_000); got != 0 {
		t.Errorf("Band(29.0 MHz) = %d, want 0", got)
	}
}
