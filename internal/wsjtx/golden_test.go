package wsjtx

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// These tests pin the exact wire bytes of the WSJT-X codec against the wire
// format described in NetworkMessage.hpp and the original FT8Commander wsjtx.py:
//
//	magic    = 0xADBCCBDA   (uint32, big-endian)
//	schema   = 2            (uint32, big-endian)
//	type     = uint32, big-endian
//	QString  = int32 length prefix + UTF-8 bytes (length -1 == null)
//
// Every multi-byte value is big-endian (Qt QDataStream default).

// headerBytes returns the expected 12-byte fixed header plus the encoded
// client-id QString, computed by hand.
func headerBytes(t PacketType, clientID string) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint32(b, 0xADBCCBDA) // magic
	b = binary.BigEndian.AppendUint32(b, 0x00000002) // schema 2
	b = binary.BigEndian.AppendUint32(b, uint32(t))  // packet type
	b = binary.BigEndian.AppendUint32(b, uint32(len(clientID)))
	b = append(b, clientID...)
	return b
}

// TestGoldenHeader asserts the leading header bytes (magic, schema, type,
// client-id QString) of an encoded packet exactly match a hand-computed slice.
func TestGoldenHeader(t *testing.T) {
	// Encode a Close packet (type 6) which is header-only.
	got := (&Close{}).Encode()
	want := headerBytes(TypeClose, ClientID) // ClientID == "AUTOFS"

	if !bytes.Equal(got, want) {
		t.Fatalf("Close header bytes:\n got % X\nwant % X", got, want)
	}

	// Spell out the exact magic/schema bytes too, so a regression in either
	// constant is caught directly.
	wantPrefix := []byte{
		0xAD, 0xBC, 0xCB, 0xDA, // magic
		0x00, 0x00, 0x00, 0x02, // schema 2
		0x00, 0x00, 0x00, 0x06, // type 6 (Close)
		0x00, 0x00, 0x00, 0x06, // QString length 6
		'A', 'U', 'T', 'O', 'F', 'S',
	}
	if !bytes.Equal(got, wantPrefix) {
		t.Fatalf("Close bytes:\n got % X\nwant % X", got, wantPrefix)
	}
}

// TestGoldenHaltTx asserts the complete byte slice for a HaltTx{Mode:false}.
// It is a header for type 8 with client id "AUTOFS" followed by a single 0x00
// bool byte.
func TestGoldenHaltTx(t *testing.T) {
	got := (&HaltTx{}).Encode() // Mode defaults to false

	want := []byte{
		0xAD, 0xBC, 0xCB, 0xDA, // magic
		0x00, 0x00, 0x00, 0x02, // schema 2
		0x00, 0x00, 0x00, 0x08, // type 8 (HaltTx)
		0x00, 0x00, 0x00, 0x06, // QString length 6
		'A', 'U', 'T', 'O', 'F', 'S', // "AUTOFS"
		0x00, // mode = false
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("HaltTx{} bytes:\n got % X\nwant % X", got, want)
	}

	// And the Mode=true variant flips only the last byte to 0x01.
	gotTrue := (&HaltTx{Mode: true}).Encode()
	wantTrue := append(append([]byte{}, want[:len(want)-1]...), 0x01)
	if !bytes.Equal(gotTrue, wantTrue) {
		t.Fatalf("HaltTx{Mode:true} bytes:\n got % X\nwant % X", gotTrue, wantTrue)
	}
}

// TestGoldenFreeText asserts the trailing QString length prefix + payload +
// send bool for NewFreeText("TEST"). NewFreeText defaults Send to true (0x01).
func TestGoldenFreeText(t *testing.T) {
	got := NewFreeText("TEST").Encode()

	header := headerBytes(TypeFreeText, ClientID)
	if !bytes.HasPrefix(got, header) {
		t.Fatalf("FreeText header:\n got % X\nwant prefix % X", got, header)
	}

	trailer := got[len(header):]
	want := []byte{
		0x00, 0x00, 0x00, 0x04, // QString length 4
		'T', 'E', 'S', 'T', // "TEST"
		0x01, // send = true
	}
	if !bytes.Equal(trailer, want) {
		t.Fatalf("FreeText trailer:\n got % X\nwant % X", trailer, want)
	}
}

// TestGoldenReplyFieldOrder decodes a Reply's own encoded bytes by hand and
// confirms the client id is "AUTOFT" (not "AUTOFS") and the field order matches
// WSReply._encode: wstime uint32, snr int32, deltaTime float64, deltaFreq
// uint32, mode QString, message QString, lowConfidence bool, modifier byte.
func TestGoldenReplyFieldOrder(t *testing.T) {
	when := time.Date(2024, 3, 15, 1, 2, 3, 456_000_000, time.UTC)
	in := &Reply{
		Time:           when,
		SNR:            -7,
		DeltaTime:      0.2,
		DeltaFrequency: 1500,
		Mode:           ModeFT8,
		Message:        "W6BSD CO8LY -07",
		LowConfidence:  false,
		Modifiers:      ShiftMod,
	}
	raw := in.Encode()
	r := bytes.NewReader(raw)

	read32 := func() uint32 {
		var v uint32
		if err := binary.Read(r, binary.BigEndian, &v); err != nil {
			t.Fatalf("read uint32: %v", err)
		}
		return v
	}
	readStr := func() string {
		n := int32(read32())
		if n <= 0 {
			return ""
		}
		b := make([]byte, n)
		if _, err := r.Read(b); err != nil {
			t.Fatalf("read string: %v", err)
		}
		return string(b)
	}
	readByte := func() byte {
		b, err := r.ReadByte()
		if err != nil {
			t.Fatalf("read byte: %v", err)
		}
		return b
	}

	if magic := read32(); magic != Magic {
		t.Fatalf("magic = %#x, want %#x", magic, Magic)
	}
	if schema := read32(); schema != Schema {
		t.Fatalf("schema = %d, want %d", schema, Schema)
	}
	if pt := PacketType(read32()); pt != TypeReply {
		t.Fatalf("type = %d, want %d (Reply)", pt, TypeReply)
	}
	if cid := readStr(); cid != "AUTOFT" {
		t.Fatalf("client id = %q, want AUTOFT (Reply uses AUTOFT, not AUTOFS)", cid)
	}
	if ws := read32(); ws != timeToWSTime(when) {
		t.Errorf("wstime = %d, want %d", ws, timeToWSTime(when))
	}
	if snr := int32(read32()); snr != -7 {
		t.Errorf("snr = %d, want -7", snr)
	}
	var dt float64
	if err := binary.Read(r, binary.BigEndian, &dt); err != nil {
		t.Fatalf("read deltaTime: %v", err)
	}
	if dt != 0.2 {
		t.Errorf("deltaTime = %v, want 0.2", dt)
	}
	if df := read32(); df != 1500 {
		t.Errorf("deltaFreq = %d, want 1500", df)
	}
	if mode := readStr(); mode != string(ModeFT8) {
		t.Errorf("mode = %q, want %q", mode, ModeFT8)
	}
	if msg := readStr(); msg != in.Message {
		t.Errorf("message = %q, want %q", msg, in.Message)
	}
	if lc := readByte(); lc != 0x00 {
		t.Errorf("lowConfidence = %#x, want 0x00", lc)
	}
	if mod := readByte(); Modifier(mod) != ShiftMod {
		t.Errorf("modifier = %#x, want %#x (ShiftMod)", mod, byte(ShiftMod))
	}
	if r.Len() != 0 {
		t.Errorf("%d trailing bytes left over", r.Len())
	}
}

// TestGoldenStatusRoundTrip hand-builds a Status (type 1) datagram field by
// field in WSStatus._decode order and confirms wsjtx.Decode reads it back. This
// is the one Out-only packet the existing tests do not round-trip.
//
// Decode order (note the three consecutive grid-ish strings: DeCall, DeGrid,
// DXGrid):
//
//	Frequency longlong, Mode str, DXCall str, Report str, TXMode str,
//	TXEnabled bool, Transmitting bool, Decoding bool, RXdf uint32, TXdf uint32,
//	DeCall str, DeGrid str, DXGrid str, TXWatchdog bool, SubMode str,
//	Fastmode bool, SOMode byte, FreqTolerance uint32, TRPeriod uint32,
//	ConfigName str, TxMessage str.
func TestGoldenStatusRoundTrip(t *testing.T) {
	var b []byte
	app32 := func(v uint32) { b = binary.BigEndian.AppendUint32(b, v) }
	app64 := func(v uint64) { b = binary.BigEndian.AppendUint64(b, v) }
	appStr := func(s string) {
		b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	appBool := func(v bool) {
		if v {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
	}

	// Header.
	app32(0xADBCCBDA) // magic
	app32(0x00000002) // schema
	app32(uint32(TypeStatus))
	appStr("WSJT-X") // client id

	// Body in WSStatus._decode order.
	app64(14074000)       // Frequency
	appStr("FT8")         // Mode
	appStr("CO8LY")       // DXCall
	appStr("-12")         // Report
	appStr("FT8")         // TXMode
	appBool(true)         // TXEnabled
	appBool(true)         // Transmitting
	appBool(false)        // Decoding
	app32(1500)           // RXdf
	app32(1600)           // TXdf
	appStr("W6BSD")       // DeCall
	appStr("CM87")        // DeGrid
	appStr("FL20")        // DXGrid
	appBool(false)        // TXWatchdog
	appStr("")            // SubMode
	appBool(false)        // Fastmode
	b = append(b, 0)      // SOMode (SONone)
	app32(50)             // FreqTolerance
	app32(15)             // TRPeriod
	appStr("Default")     // ConfigName
	appStr("W6BSD CO8LY") // TxMessage

	msg, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	st, ok := msg.(*Status)
	if !ok {
		t.Fatalf("got %T, want *Status", msg)
	}

	if st.ClientID != "WSJT-X" {
		t.Errorf("ClientID = %q, want WSJT-X", st.ClientID)
	}
	if st.Frequency != 14074000 {
		t.Errorf("Frequency = %d, want 14074000", st.Frequency)
	}
	if st.Mode != "FT8" || st.TXMode != "FT8" {
		t.Errorf("Mode/TXMode = %q/%q, want FT8/FT8", st.Mode, st.TXMode)
	}
	if st.DXCall != "CO8LY" {
		t.Errorf("DXCall = %q, want CO8LY", st.DXCall)
	}
	if st.Report != "-12" {
		t.Errorf("Report = %q, want -12", st.Report)
	}
	if !st.TXEnabled || !st.Transmitting || st.Decoding {
		t.Errorf("TXEnabled/Transmitting/Decoding = %v/%v/%v, want true/true/false",
			st.TXEnabled, st.Transmitting, st.Decoding)
	}
	if st.RXdf != 1500 || st.TXdf != 1600 {
		t.Errorf("RXdf/TXdf = %d/%d, want 1500/1600", st.RXdf, st.TXdf)
	}
	if st.DeCall != "W6BSD" || st.DeGrid != "CM87" || st.DXGrid != "FL20" {
		t.Errorf("DeCall/DeGrid/DXGrid = %q/%q/%q, want W6BSD/CM87/FL20",
			st.DeCall, st.DeGrid, st.DXGrid)
	}
	if st.TXWatchdog || st.Fastmode {
		t.Errorf("TXWatchdog/Fastmode = %v/%v, want false/false", st.TXWatchdog, st.Fastmode)
	}
	if st.SOMode != SONone {
		t.Errorf("SOMode = %d, want SONone", st.SOMode)
	}
	if st.FreqTolerance != 50 || st.TRPeriod != 15 {
		t.Errorf("FreqTolerance/TRPeriod = %d/%d, want 50/15", st.FreqTolerance, st.TRPeriod)
	}
	if st.ConfigName != "Default" {
		t.Errorf("ConfigName = %q, want Default", st.ConfigName)
	}
	if st.TxMessage != "W6BSD CO8LY" {
		t.Errorf("TxMessage = %q, want %q", st.TxMessage, "W6BSD CO8LY")
	}
}

// TestGoldenJulian pins the Julian-day conversion against a hand-computed value:
// 2000-01-01 00:00:00 UTC maps to Julian day 2451545 (julianOrigin) with 0
// msecs, matching from_julian/to_julian in wsjtx.py.
func TestGoldenJulian(t *testing.T) {
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	jday, msecs := toJulian(epoch)
	if jday != julianOrigin {
		t.Errorf("toJulian(2000-01-01) jday = %d, want %d", jday, julianOrigin)
	}
	if msecs != 0 {
		t.Errorf("toJulian(2000-01-01) msecs = %d, want 0", msecs)
	}
	if julianOrigin != 2451545 {
		t.Errorf("julianOrigin = %d, want 2451545", julianOrigin)
	}

	// 2000-01-02 12:00:00 is exactly one day plus 12h after the epoch:
	// jday = 2451546, msecs = 12*3600*1000 = 43_200_000.
	d := time.Date(2000, 1, 2, 12, 0, 0, 0, time.UTC)
	jday, msecs = toJulian(d)
	if jday != 2451546 || msecs != 43_200_000 {
		t.Errorf("toJulian(2000-01-02 12:00) = (%d,%d), want (2451546,43200000)", jday, msecs)
	}
	// And fromJulian reverses it.
	if got := fromJulian(2451546, 43_200_000); !got.Equal(d) {
		t.Errorf("fromJulian(2451546,43200000) = %v, want %v", got, d)
	}
	// fromJulian of the origin returns the epoch.
	if got := fromJulian(julianOrigin, 0); !got.Equal(epoch) {
		t.Errorf("fromJulian(origin,0) = %v, want %v", got, epoch)
	}
}

// TestGoldenWSTime pins the wstime (milliseconds since UTC midnight) helpers
// against a hand-computed value on the current UTC day.
func TestGoldenWSTime(t *testing.T) {
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// 01:02:03.456 past midnight = ((1*3600)+(2*60)+3)*1000 + 456 ms.
	at := midnight.Add(1*time.Hour + 2*time.Minute + 3*time.Second + 456*time.Millisecond)
	wantMs := uint32((1*3600+2*60+3)*1000 + 456)
	if got := timeToWSTime(at); got != wantMs {
		t.Errorf("timeToWSTime = %d, want %d", got, wantMs)
	}
	// wsTimeToTime anchors on today's midnight, so it reconstructs `at`.
	if got := wsTimeToTime(wantMs); !got.Equal(at) {
		t.Errorf("wsTimeToTime(%d) = %v, want %v", wantMs, got, at)
	}
	// Midnight itself is 0.
	if got := timeToWSTime(midnight); got != 0 {
		t.Errorf("timeToWSTime(midnight) = %d, want 0", got)
	}
}

// TestGoldenFloat64Encoding confirms float64 fields use IEEE-754 big-endian, so
// the Reply DeltaTime bytes are exactly what encoding/binary would emit.
func TestGoldenFloat64Encoding(t *testing.T) {
	w := &writer{}
	w.float64(0.2)
	want := make([]byte, 8)
	binary.BigEndian.PutUint64(want, math.Float64bits(0.2))
	if !bytes.Equal(w.bytes(), want) {
		t.Fatalf("float64(0.2) = % X, want % X", w.bytes(), want)
	}
}
