package sequencer

import "time"

// sequenceSeconds maps a TX mode to the set of UTC seconds-of-minute at which a
// new transmit sequence may start. Port of SEQUENCE_TIME in ft8ctrl.py.
//
// This whole-second model only works for modes whose T/R period is a whole
// number of seconds. Faster modes (FT2, 3.75 s) live in subSecondPeriods below.
var sequenceSeconds = map[string]map[int]bool{
	"FT8": {2: true, 17: true, 32: true, 47: true},
	"FT4": {0: true, 6: true, 12: true, 18: true, 24: true, 30: true,
		36: true, 42: true, 48: true, 54: true},
}

// subSecondPeriods maps a TX mode whose T/R period is not a whole number of
// seconds to that period. The sequencer fires once per period boundary (aligned
// to the UTC epoch) instead of using sequenceSeconds. FT2's 3.75 s period gives
// 16 transmit windows per minute, which whole-second granularity can't express.
var subSecondPeriods = map[string]time.Duration{
	"FT2": 3750 * time.Millisecond,
}

// txTracker counts how many consecutive Status packets report the same transmit
// message, so the daemon can give up after tx_retries. Port of the
// current_retries / last_tx_message logic in the Status case of ft8ctrl.py.
type txTracker struct {
	lastMsg string
	retries int
	max     int
}

// observe folds in one Status packet and reports whether the retry limit has
// been reached (in which case the caller should halt transmission). It mirrors
// the original ordering exactly, including the reset-on-exceed behaviour.
func (t *txTracker) observe(decoding, transmitting bool, txMsg string) (exceeded bool) {
	tx := !decoding && transmitting
	if tx && t.lastMsg == txMsg {
		if t.retries >= t.max {
			t.retries = 0
			return true
		}
	} else if tx && t.lastMsg != txMsg {
		t.retries = 0
	}
	if tx {
		t.retries++
		t.lastMsg = txMsg
	}
	return false
}

// reset clears the retry counter, called when a fresh station is called.
func (t *txTracker) reset() { t.retries = 0 }
