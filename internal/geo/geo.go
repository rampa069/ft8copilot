package geo

import (
	"fmt"
	"math"
	"strings"
)

// earthRadiusKm is the mean radius of the Earth in kilometers.
const earthRadiusKm = 6371.0

// Point represents a geographic coordinate in degrees.
type Point struct {
	Lat, Lon float64
}

// haversine returns sin(val/2)**2, the haversine of an angle in radians.
func haversine(val float64) float64 {
	s := math.Sin(val / 2)
	return s * s
}

// Distance returns the great-circle distance in kilometers between orig and
// dest using the haversine formula.
func Distance(orig, dest Point) float64 {
	dphi := degToRad(dest.Lat - orig.Lat)
	dlambda := degToRad(dest.Lon - orig.Lon)
	phi1 := degToRad(orig.Lat)
	phi2 := degToRad(dest.Lat)

	a := haversine(dphi) + math.Cos(phi1)*math.Cos(phi2)*haversine(dlambda)
	return 2 * earthRadiusKm * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// Azimuth returns the bearing in whole degrees of dest as seen from orig.
//
// It mirrors the reference Python implementation: the floating-point bearing is
// truncated toward zero and the absolute value is returned as an int.
func Azimuth(orig, dest Point) int {
	dLon := dest.Lon - orig.Lon
	x := math.Cos(degToRad(dest.Lat)) * math.Sin(degToRad(dLon))
	y := math.Cos(degToRad(orig.Lat))*math.Sin(degToRad(dest.Lat)) -
		math.Sin(degToRad(orig.Lat))*math.Cos(degToRad(dest.Lat))*math.Cos(degToRad(dLon))
	brng := radToDeg(math.Atan2(x, y))
	// math.Trunc truncates toward zero, matching Python's int().
	return int(math.Abs(math.Trunc(brng)))
}

// GridToLatLon converts a Maidenhead grid locator to a Point.
//
// An empty locator returns the zero Point with a nil error, matching the
// reference behavior. The locator must be 2, 4, 6 or 8 characters long;
// otherwise an error is returned.
func GridToLatLon(grid string) (Point, error) {
	if grid == "" {
		return Point{0, 0}, nil
	}

	m := strings.ToUpper(strings.TrimSpace(grid))
	n := len(m)
	if n != 2 && n != 4 && n != 6 && n != 8 {
		return Point{}, fmt.Errorf("locator length error: 2, 4, 6 or 8 characters accepted")
	}

	const charA = 'A'
	lon := -180.0
	lat := -90.0

	lon += float64(rune(m[0])-charA) * 20
	lat += float64(rune(m[1])-charA) * 10

	if n >= 4 {
		d2, err := digit(m[2])
		if err != nil {
			return Point{}, err
		}
		d3, err := digit(m[3])
		if err != nil {
			return Point{}, err
		}
		lon += float64(d2) * 2
		lat += float64(d3)
	}
	if n >= 6 {
		lon += float64(rune(m[4])-charA) * 5.0 / 60
		lat += float64(rune(m[5])-charA) * 2.5 / 60
	}
	if n >= 8 {
		d6, err := digit(m[6])
		if err != nil {
			return Point{}, err
		}
		d7, err := digit(m[7])
		if err != nil {
			return Point{}, err
		}
		lon += float64(d6) * 5.0 / 600
		lat += float64(d7) * 2.5 / 600
	}

	return Point{Lat: lat, Lon: lon}, nil
}

// digit parses a single ASCII decimal digit, mirroring Python's int(str).
func digit(b byte) (int, error) {
	if b < '0' || b > '9' {
		return 0, fmt.Errorf("invalid locator digit: %q", string(b))
	}
	return int(b - '0'), nil
}

func degToRad(d float64) float64 { return d * math.Pi / 180 }

func radToDeg(r float64) float64 { return r * 180 / math.Pi }
