package wsjtx

import (
	"encoding/binary"
	"io"
	"math"
	"time"
)

// julianOrigin is the Julian Day Number for 2000-01-01, the epoch Qt uses for
// the date part of a serialized QDateTime.
const julianOrigin = 2451545

// reader consumes a big-endian QDataStream byte buffer. Once an error occurs
// (typically a short read) every subsequent read is a no-op and returns the
// zero value, so callers can decode a whole packet and check err() once.
type reader struct {
	buf []byte
	pos int
	err error
}

func newReader(buf []byte) *reader { return &reader{buf: buf} }

func (r *reader) readErr() error { return r.err }

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if r.pos+n > len(r.buf) {
		r.err = io.ErrUnexpectedEOF
		return nil
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *reader) remaining() int { return len(r.buf) - r.pos }

func (r *reader) byteVal() byte {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *reader) boolVal() bool { return r.byteVal() != 0 }

func (r *reader) uint32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *reader) int32() int32 { return int32(r.uint32()) }

func (r *reader) uint64() uint64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func (r *reader) float64() float64 { return math.Float64frombits(r.uint64()) }

// str reads a QString. A length of 0xffffffff (-1) denotes a null string, which
// we surface as the empty string.
func (r *reader) str() string {
	n := r.int32()
	if n <= 0 { // null (-1) or empty
		return ""
	}
	b := r.take(int(n))
	if b == nil {
		return ""
	}
	return string(b)
}

// dateTime reads a serialized QDateTime and returns it as a UTC time.Time.
func (r *reader) dateTime() time.Time {
	jday := r.uint64()
	msecs := r.uint32()
	spec := r.byteVal()
	if spec == 2 {
		_ = r.int32() // offset from UTC; ignored, matches the original
	}
	return fromJulian(int64(jday), msecs)
}

// writer accumulates a big-endian QDataStream byte buffer.
type writer struct {
	buf []byte
}

func (w *writer) bytes() []byte { return w.buf }

func (w *writer) byteVal(v byte) { w.buf = append(w.buf, v) }

func (w *writer) boolVal(v bool) {
	if v {
		w.byteVal(1)
	} else {
		w.byteVal(0)
	}
}

func (w *writer) uint16(v uint16) { w.buf = binary.BigEndian.AppendUint16(w.buf, v) }
func (w *writer) uint32(v uint32) { w.buf = binary.BigEndian.AppendUint32(w.buf, v) }
func (w *writer) int32(v int32)   { w.uint32(uint32(v)) }
func (w *writer) uint64(v uint64) { w.buf = binary.BigEndian.AppendUint64(w.buf, v) }
func (w *writer) float64(v float64) {
	w.uint64(math.Float64bits(v))
}

// str writes a QString (int32 length prefix followed by the UTF-8 bytes).
func (w *writer) str(s string) {
	w.int32(int32(len(s)))
	w.buf = append(w.buf, s...)
}

// dateTime writes t as a serialized QDateTime with TimeSpec=1 (UTC), matching
// the original which only ever emits UTC timestamps.
func (w *writer) dateTime(t time.Time) {
	jday, msecs := toJulian(t)
	w.uint64(uint64(jday))
	w.uint32(msecs)
	w.byteVal(1) // Qt::UTC
}

// fromJulian converts a Qt QDateTime date (Julian day) and time (msecs since
// midnight) into a UTC time.Time. Not valid for dates before 2000.
func fromJulian(jday int64, msecs uint32) time.Time {
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	day := epoch.AddDate(0, 0, int(jday-julianOrigin))
	return day.Add(time.Duration(msecs) * time.Millisecond)
}

// toJulian converts a UTC time.Time into a Qt QDateTime (Julian day, msecs
// since midnight). Not valid for dates before 2000.
func toJulian(t time.Time) (jday int64, msecs uint32) {
	t = t.UTC()
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	delta := t.Sub(epoch)
	days := int64(delta / (24 * time.Hour))
	jday = days + julianOrigin
	secOfDay := delta - time.Duration(days)*24*time.Hour
	msecs = uint32(secOfDay / time.Millisecond)
	return jday, msecs
}

// timeToWSTime converts a time to milliseconds since today's UTC midnight, the
// representation WSJT-X uses for the QTime field on Decode/Reply messages.
func timeToWSTime(t time.Time) uint32 {
	t = t.UTC()
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return uint32(t.Sub(midnight) / time.Millisecond)
}

// wsTimeToTime converts milliseconds since midnight into a UTC time.Time
// anchored on the current UTC day.
func wsTimeToTime(ms uint32) time.Time {
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return midnight.Add(time.Duration(ms) * time.Millisecond)
}
