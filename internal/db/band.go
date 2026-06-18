package db

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
