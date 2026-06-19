package db

import (
	"strconv"
	"strings"
)

// BandMetersFromName parses an ADIF band name (e.g. "20m", "160m", "2m") into
// the band in metres. It returns 0 for names it cannot represent as an integer
// number of metres — notably the centimetre bands ("70cm") and anything
// unparseable — so callers can fall back to the dial frequency via Band.
func BandMetersFromName(name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.HasSuffix(name, "cm") {
		return 0
	}
	name = strings.TrimSuffix(name, "m")
	n, err := strconv.Atoi(name)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// bandByMHz maps the integer MHz part of a dial frequency to the ham-radio band
// in meters. Port of get_band in dbutils.py.
var bandByMHz = map[uint64]int{
	1:  160,
	3:  80,
	7:  40,
	10: 30,
	14: 20,
	18: 17,
	21: 15,
	24: 12,
	28: 10,
	50: 6,
}

// Band returns the band in meters for a dial frequency in Hz, or 0 if the
// frequency does not fall on a known band.
func Band(freqHz uint64) int {
	return bandByMHz[freqHz/1_000_000]
}
