package geo

import (
	"math"
	"testing"
)

// These tests pin Distance/Azimuth/GridToLatLon to the exact formulas in the
// original FT8Commander geo.py:
//
//	distance: haversine great-circle with Earth radius r = 6371 km
//	azimuth:  abs(int(degrees(atan2(x, y)))), i.e. truncate toward zero
//	grid:     Maidenhead decode (-180/-90 origin, 20/10, 2/1, 5'/2.5', ...)
//
// Grid lat/lon are exact decimal arithmetic so they get a tight tolerance;
// distance gets ~0.5 km because the formula is reproduced independently below.

const gridEps = 1e-9

// refDistance reproduces geo.py distance() independently so the test asserts the
// Go port against a from-scratch evaluation of the same formula.
func refDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const radius = 6371.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	hav := func(v float64) float64 { s := math.Sin(v / 2); return s * s }
	dphi := rad(lat2 - lat1)
	dlambda := rad(lon2 - lon1)
	phi1, phi2 := rad(lat1), rad(lat2)
	a := hav(dphi) + math.Cos(phi1)*math.Cos(phi2)*hav(dlambda)
	return 2 * radius * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// refAzimuth reproduces geo.py azimuth() independently.
func refAzimuth(lat1, lon1, lat2, lon2 float64) int {
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLon := lon2 - lon1
	x := math.Cos(rad(lat2)) * math.Sin(rad(dLon))
	y := math.Cos(rad(lat1))*math.Sin(rad(lat2)) -
		math.Sin(rad(lat1))*math.Cos(rad(lat2))*math.Cos(rad(dLon))
	brng := math.Atan2(x, y) * 180 / math.Pi
	return int(math.Abs(math.Trunc(brng)))
}

func TestGridToLatLonParity(t *testing.T) {
	tests := []struct {
		grid string
		want Point
	}{
		// CM87 (the worked example): lon = -180 + (C-A=2)*20 + 8*2 = -180+40+16 = -124
		//                            lat = -90  + (M-A=12)*10 + 7   = -90+120+7  = 37
		{"CM87", Point{Lat: 37, Lon: -124}},
		// FN20: lon = -180 + (F-A=5)*20 + 2*2 = -180+100+4 = -76
		//       lat = -90  + (N-A=13)*10 + 0  = -90+130    = 40
		{"FN20", Point{Lat: 40, Lon: -76}},
		// JJ00: lon = -180 + (J-A=9)*20 + 0 = -180+180 = 0
		//       lat = -90  + (J-A=9)*10 + 0 = -90+90   = 0
		{"JJ00", Point{Lat: 0, Lon: 0}},
		// FM18 (the operator grid used elsewhere):
		//   lon = -180 + (F-A=5)*20 + 1*2 = -180+100+2 = -78
		//   lat = -90  + (M-A=12)*10 + 8  = -90+120+8  = 38
		{"FM18", Point{Lat: 38, Lon: -78}},
		// AA00: the lower-left corner of the grid system.
		{"AA00", Point{Lat: -90, Lon: -180}},
	}
	for _, tc := range tests {
		got, err := GridToLatLon(tc.grid)
		if err != nil {
			t.Fatalf("GridToLatLon(%q): %v", tc.grid, err)
		}
		if math.Abs(got.Lat-tc.want.Lat) > gridEps || math.Abs(got.Lon-tc.want.Lon) > gridEps {
			t.Errorf("GridToLatLon(%q) = %+v, want %+v", tc.grid, got, tc.want)
		}
	}
}

func TestGridToLatLon6CharParity(t *testing.T) {
	// CM87wj: from CM87 (37, -124) add the sub-square offsets.
	//   lon = -124 + (W-A=22)*5/60   = -124 + 1.833333... = -122.16666...
	//   lat = 37   + (J-A=9)*2.5/60  = 37   + 0.375        = 37.375
	got, err := GridToLatLon("CM87wj")
	if err != nil {
		t.Fatalf("GridToLatLon: %v", err)
	}
	wantLon := -124.0 + 22.0*5.0/60.0
	wantLat := 37.0 + 9.0*2.5/60.0
	if math.Abs(got.Lon-wantLon) > gridEps || math.Abs(got.Lat-wantLat) > gridEps {
		t.Errorf("GridToLatLon(CM87wj) = %+v, want Lat=%v Lon=%v", got, wantLat, wantLon)
	}
}

func TestDistanceParity(t *testing.T) {
	// CM87 (37,-124) -> FN20 (40,-76): SF area to mid-Atlantic US.
	a, _ := GridToLatLon("CM87")
	b, _ := GridToLatLon("FN20")
	got := Distance(a, b)
	want := refDistance(a.Lat, a.Lon, b.Lat, b.Lon)
	if math.Abs(got-want) > 0.5 {
		t.Errorf("Distance(CM87,FN20) = %v, want %v (formula)", got, want)
	}

	// A simple exact case: 90 degrees of longitude along the equator is a
	// quarter of the great circle: (2*pi*6371)/4 = 10007.543398... km.
	eq := Distance(Point{0, 0}, Point{0, 90})
	wantEq := 2 * math.Pi * 6371 / 4
	if math.Abs(eq-wantEq) > 0.5 {
		t.Errorf("Distance(equator 90deg) = %v, want %v", eq, wantEq)
	}

	// Antipodal-ish: 180 degrees along the equator is half the great circle.
	half := Distance(Point{0, 0}, Point{0, 180})
	wantHalf := math.Pi * 6371
	if math.Abs(half-wantHalf) > 1.0 {
		t.Errorf("Distance(equator 180deg) = %v, want %v", half, wantHalf)
	}
}

func TestAzimuthParity(t *testing.T) {
	a, _ := GridToLatLon("CM87")
	b, _ := GridToLatLon("FN20")
	got := Azimuth(a, b)
	want := refAzimuth(a.Lat, a.Lon, b.Lat, b.Lon)
	if got != want {
		t.Errorf("Azimuth(CM87,FN20) = %d, want %d (formula)", got, want)
	}

	// Cardinal directions from the equator/prime-meridian:
	//   due east  (0,0)->(0,10): atan2(sin(10deg)*cos0, ...) = +90 -> 90
	//   due west  (0,0)->(0,-10): -90 -> abs(int(-90)) = 90
	//   due north (0,0)->(10,0): 0
	if az := Azimuth(Point{0, 0}, Point{0, 10}); az != 90 {
		t.Errorf("Azimuth due east = %d, want 90", az)
	}
	if az := Azimuth(Point{0, 0}, Point{0, -10}); az != 90 {
		t.Errorf("Azimuth due west = %d, want 90 (abs of -90)", az)
	}
	if az := Azimuth(Point{0, 0}, Point{10, 0}); az != 0 {
		t.Errorf("Azimuth due north = %d, want 0", az)
	}
}
