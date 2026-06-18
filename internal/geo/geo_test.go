package geo

import (
	"math"
	"testing"
)

func TestGridToLatLon(t *testing.T) {
	tests := []struct {
		name    string
		grid    string
		want    Point
		wantErr bool
	}{
		{"empty", "", Point{0, 0}, false},
		{"CM87", "CM87", Point{Lat: 37, Lon: -124}, false},
		// FN20: lon = -180 + (F-A)*20 + 2*2 = -180+100+4 = -76
		//       lat = -90 + (N-A)*10 + 0   = -90+130    = 40
		{"FN20", "FN20", Point{Lat: 40, Lon: -76}, false},
		// CM (2 chars): lon = -180 + 40 = -140 ; lat = -90 + 120 = 30
		{"CM", "CM", Point{Lat: 30, Lon: -140}, false},
		{"whitespace lower", "  cm87  ", Point{Lat: 37, Lon: -124}, false},
		{"len3 error", "ABC", Point{}, true},
		{"len5 error", "CM871", Point{}, true},
	}

	const eps = 1e-9
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GridToLatLon(tc.grid)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (point %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got.Lat-tc.want.Lat) > eps || math.Abs(got.Lon-tc.want.Lon) > eps {
				t.Errorf("GridToLatLon(%q) = %+v, want %+v", tc.grid, got, tc.want)
			}
		})
	}
}

func TestGridToLatLon6Char(t *testing.T) {
	// CM87wj is a common San Francisco-area locator.
	got, err := GridToLatLon("CM87wj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// lon = -124 + (W-A)*5/60 = -124 + 22*5/60
	// lat = 37   + (J-A)*2.5/60 = 37 + 9*2.5/60
	wantLon := -124 + float64(22)*5.0/60
	wantLat := 37 + float64(9)*2.5/60
	if math.Abs(got.Lat-wantLat) > 1e-9 || math.Abs(got.Lon-wantLon) > 1e-9 {
		t.Errorf("GridToLatLon(CM87wj) = %+v, want Lat=%v Lon=%v", got, wantLat, wantLon)
	}
}

func TestDistance(t *testing.T) {
	// Identical points => 0.
	if d := Distance(Point{37, -122}, Point{37, -122}); math.Abs(d) > 1e-9 {
		t.Errorf("Distance between identical points = %v, want 0", d)
	}

	// SF ~ NYC, roughly 4130 km. Allow a generous tolerance.
	d := Distance(Point{37, -122}, Point{40, -74})
	if math.Abs(d-4150) > 50 {
		t.Errorf("Distance(SF, NYC) = %v km, want ~4150 (+/-50)", d)
	}

	// Distance is symmetric.
	a := Distance(Point{0, 0}, Point{10, 10})
	b := Distance(Point{10, 10}, Point{0, 0})
	if math.Abs(a-b) > 1e-9 {
		t.Errorf("Distance not symmetric: %v vs %v", a, b)
	}
}

func TestAzimuth(t *testing.T) {
	// Due east from the equator: dest directly east => bearing ~90.
	if az := Azimuth(Point{0, 0}, Point{0, 10}); az != 90 {
		t.Errorf("Azimuth due east = %d, want 90", az)
	}

	// Due north => bearing 0.
	if az := Azimuth(Point{0, 0}, Point{10, 0}); az != 0 {
		t.Errorf("Azimuth due north = %d, want 0", az)
	}

	// Result is always a non-negative int in [0, 360).
	for _, tc := range []struct{ o, d Point }{
		{Point{0, 0}, Point{0, -10}},
		{Point{37, -122}, Point{40, -74}},
		{Point{-33, 151}, Point{51, 0}},
	} {
		az := Azimuth(tc.o, tc.d)
		if az < 0 || az >= 360 {
			t.Errorf("Azimuth(%+v,%+v) = %d, out of [0,360)", tc.o, tc.d, az)
		}
	}
}
