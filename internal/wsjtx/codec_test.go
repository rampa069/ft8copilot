package wsjtx

import (
	"testing"
	"time"
)

func TestHeartbeatRoundTrip(t *testing.T) {
	raw := NewHeartbeat().Encode()
	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	hb, ok := msg.(*Heartbeat)
	if !ok {
		t.Fatalf("got %T, want *Heartbeat", msg)
	}
	if hb.ClientID != ClientID {
		t.Errorf("ClientID = %q, want %q", hb.ClientID, ClientID)
	}
	if hb.MaxSchema != Schema || hb.Version != Version || hb.Revision != Revision {
		t.Errorf("got %+v, want schema=%d version=%q revision=%q", hb, Schema, Version, Revision)
	}
}

func TestQSOLoggedRoundTrip(t *testing.T) {
	on := time.Date(2024, 3, 15, 12, 30, 45, 123_000_000, time.UTC)
	off := on.Add(90 * time.Second)
	in := &QSOLogged{
		DateTimeOff:    off,
		DXCall:         "CO8LY",
		DXGrid:         "FL20",
		DialFrequency:  14074000,
		Mode:           "FT8",
		ReportSent:     "-10",
		ReportReceived: "-12",
		TXPower:        "30",
		Comments:       "[ft8ctrl] test",
		Name:           "",
		DateTimeOn:     on,
		MyCall:         "W6BSD",
		MyGrid:         "CM87",
	}
	msg, err := Decode(in.Encode())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, ok := msg.(*QSOLogged)
	if !ok {
		t.Fatalf("got %T, want *QSOLogged", msg)
	}
	if got.DXCall != in.DXCall || got.DXGrid != in.DXGrid || got.DialFrequency != in.DialFrequency {
		t.Errorf("call/grid/freq mismatch: %+v", got)
	}
	if got.Mode != in.Mode || got.Comments != in.Comments || got.MyCall != in.MyCall {
		t.Errorf("mode/comments/mycall mismatch: %+v", got)
	}
	if !got.DateTimeOn.Equal(in.DateTimeOn) {
		t.Errorf("DateTimeOn = %v, want %v", got.DateTimeOn, in.DateTimeOn)
	}
	if !got.DateTimeOff.Equal(in.DateTimeOff) {
		t.Errorf("DateTimeOff = %v, want %v", got.DateTimeOff, in.DateTimeOff)
	}
}

func TestReplyEncode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	in := &Reply{
		Time:           now,
		SNR:            -7,
		DeltaTime:      0.2,
		DeltaFrequency: 1500,
		Mode:           ModeFT8,
		Message:        "W6BSD CO8LY -07",
		Modifiers:      ShiftMod,
	}
	r := newReader(in.Encode())
	if r.uint32() != Magic {
		t.Fatal("bad magic")
	}
	r.uint32() // schema
	if PacketType(r.uint32()) != TypeReply {
		t.Fatal("type != Reply")
	}
	if cid := r.str(); cid != replyClientID {
		t.Errorf("client id = %q, want %q", cid, replyClientID)
	}
	if ws := r.uint32(); ws != timeToWSTime(now) {
		t.Errorf("wstime = %d, want %d", ws, timeToWSTime(now))
	}
	if snr := r.int32(); snr != -7 {
		t.Errorf("snr = %d, want -7", snr)
	}
	if dt := r.float64(); dt != 0.2 {
		t.Errorf("deltaTime = %v, want 0.2", dt)
	}
	if df := r.uint32(); df != 1500 {
		t.Errorf("deltaFreq = %d, want 1500", df)
	}
	if mode := r.str(); mode != string(ModeFT8) {
		t.Errorf("mode = %q, want %q", mode, ModeFT8)
	}
	if msg := r.str(); msg != in.Message {
		t.Errorf("message = %q, want %q", msg, in.Message)
	}
	r.boolVal() // low confidence
	if mod := r.byteVal(); Modifier(mod) != ShiftMod {
		t.Errorf("modifier = %d, want %d", mod, ShiftMod)
	}
	if r.readErr() != nil {
		t.Fatalf("reader error: %v", r.readErr())
	}
}

func TestFreeTextEncode(t *testing.T) {
	in := NewFreeText("CQ EA5IUE IM76")
	if !in.Send {
		t.Fatal("NewFreeText should default Send=true")
	}
	r := newReader(in.Encode())
	if r.uint32() != Magic {
		t.Fatal("bad magic")
	}
	r.uint32() // schema
	if PacketType(r.uint32()) != TypeFreeText {
		t.Fatal("type != FreeText")
	}
	if cid := r.str(); cid != ClientID {
		t.Errorf("client id = %q, want %q", cid, ClientID)
	}
	if txt := r.str(); txt != "CQ EA5IUE IM76" {
		t.Errorf("text = %q, want %q", txt, "CQ EA5IUE IM76")
	}
	if !r.boolVal() {
		t.Error("send = false, want true")
	}
	if r.readErr() != nil {
		t.Fatalf("reader error: %v", r.readErr())
	}
}

func TestHaltTxEncode(t *testing.T) {
	raw := (&HaltTx{Mode: true}).Encode()
	r := newReader(raw)
	r.uint32() // magic
	r.uint32() // schema
	if PacketType(r.uint32()) != TypeHaltTx {
		t.Fatal("type != HaltTx")
	}
	r.str() // client id
	if !r.boolVal() {
		t.Error("mode = false, want true")
	}
}

func TestJulianRoundTrip(t *testing.T) {
	want := time.Date(2024, 3, 15, 12, 30, 45, 123_000_000, time.UTC)
	jday, msecs := toJulian(want)
	got := fromJulian(jday, msecs)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v (jday=%d msecs=%d)", got, want, jday, msecs)
	}
}

func TestWSTimeRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	ms := timeToWSTime(now)
	got := wsTimeToTime(ms)
	if timeToWSTime(got) != ms {
		t.Errorf("wstime not stable: %d vs %d", timeToWSTime(got), ms)
	}
}

func TestDecodeBadMagic(t *testing.T) {
	if _, err := Decode([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}); err != ErrNotWSJTX {
		t.Errorf("err = %v, want ErrNotWSJTX", err)
	}
}

func TestDecodeUnknownType(t *testing.T) {
	w := &writer{}
	writeHeader(w, TypeWSPRDecode, ClientID)
	_, err := Decode(w.bytes())
	var ute *UnknownTypeError
	if err == nil {
		t.Fatal("want error for unknown type")
	}
	if e, ok := err.(*UnknownTypeError); ok {
		ute = e
	}
	if ute == nil || ute.Type != TypeWSPRDecode {
		t.Errorf("err = %v, want UnknownTypeError(%d)", err, TypeWSPRDecode)
	}
}
