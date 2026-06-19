package sequencer

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/selector"
	"github.com/rampa069/ft8copilot/internal/wsjtx"
)

func TestParseMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want parsed
	}{
		{"CQ CO8LY FL11", parsed{kind: msgCQ, call: "CO8LY", grid: "FL11"}},
		{"CQ DX W6BSD CM87", parsed{kind: msgCQ, call: "W6BSD", grid: "CM87", extra: "DX"}},
		{"CQ POTA K1ABC FN42", parsed{kind: msgCQ, call: "K1ABC", grid: "FN42", extra: "POTA"}},
		{"CQ W1AW", parsed{kind: msgCQ, call: "W1AW"}}, // broken CQ, no grid
		{"W6BSD CO8LY -10", parsed{kind: msgReply, to: "W6BSD", call: "CO8LY"}},
		{"W6BSD CO8LY RR73", parsed{kind: msgReply, to: "W6BSD", call: "CO8LY", rr73: true}},
		{"W6BSD CO8LY 73", parsed{kind: msgReply, to: "W6BSD", call: "CO8LY", rr73: true}},
		{"W6BSD CO8LY RRR", parsed{kind: msgReply, to: "W6BSD", call: "CO8LY"}}, // RRR is not a sign-off
		{"CO8LY/P W6BSD -07", parsed{kind: msgReply, to: "CO8LY", call: "W6BSD"}},
		{"random noise", parsed{kind: msgNone}},
	}
	for _, c := range cases {
		got := parseMessage(c.msg)
		if got != c.want {
			t.Errorf("parseMessage(%q) = %+v, want %+v", c.msg, got, c.want)
		}
	}
}

func TestTxTracker(t *testing.T) {
	tr := txTracker{max: 3}
	const msg = "CO8LY W6BSD -10"
	// First three observations of the same TX message must not exceed.
	for i := 0; i < 3; i++ {
		if tr.observe(false, true, msg) {
			t.Fatalf("exceeded too early at i=%d", i)
		}
	}
	// The fourth (retries now >= max) trips the limit and resets.
	if !tr.observe(false, true, msg) {
		t.Fatal("expected retry limit to be reached")
	}
	if tr.retries != 0 {
		t.Errorf("retries = %d after exceed, want 0", tr.retries)
	}

	// A new TX message resets the counter.
	tr.observe(false, true, msg)
	tr.observe(false, true, "OTHER MSG")
	if tr.retries != 1 {
		t.Errorf("retries = %d after message change, want 1", tr.retries)
	}
	// Decoding=true means not transmitting: no counting.
	before := tr.retries
	tr.observe(true, true, "OTHER MSG")
	if tr.retries != before {
		t.Errorf("retries changed while decoding")
	}
}

func TestSequenceSeconds(t *testing.T) {
	if !sequenceSeconds["FT8"][2] || !sequenceSeconds["FT8"][47] {
		t.Error("FT8 sequence seconds wrong")
	}
	if sequenceSeconds["FT8"][3] {
		t.Error("FT8 should not fire at second 3")
	}
	if !sequenceSeconds["FT4"][0] || !sequenceSeconds["FT4"][54] {
		t.Error("FT4 sequence seconds wrong")
	}
}

// newTestSeq builds a Sequencer without a socket, for handler-logic tests.
func newTestSeq(cmdBuf int) (*Sequencer, chan db.Command) {
	ch := make(chan db.Command, cmdBuf)
	s := &Sequencer{
		mycall:     "W6BSD",
		cmds:       ch,
		log:        slog.New(slog.NewTextHandler(discard{}, nil)),
		sequence:   map[int]bool{},
		tracker:    txTracker{max: 5},
		lastSecond: -1,
	}
	return s, ch
}

// setChain stores a chain into the atomic holder, for tests that build a
// Sequencer by hand and need to seed or swap its selector chain.
func setChain(s *Sequencer, c selector.Chain) {
	s.chain.Store(&c)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestHandleDecodeCQEmitsInsert(t *testing.T) {
	s, ch := newTestSeq(4)
	s.frequency = 14074000 // 20m
	s.handleDecode(&wsjtx.DecodeMsg{
		Time:           time.Now().UTC(),
		SNR:            -10,
		DeltaFrequency: 1500,
		Mode:           wsjtx.ModeFT8,
		Message:        "CQ CO8LY FL11",
	})
	cmd := <-ch
	ins, ok := cmd.(db.InsertCmd)
	if !ok {
		t.Fatalf("got %T, want InsertCmd", cmd)
	}
	if ins.Spot.Call != "CO8LY" || ins.Spot.Grid != "FL11" {
		t.Errorf("spot = %+v, want call CO8LY grid FL11", ins.Spot)
	}
	if ins.Spot.Band != 20 {
		t.Errorf("band = %d, want 20", ins.Spot.Band)
	}
	if ins.Spot.Packet.SNR != -10 || ins.Spot.Packet.Mode != "~" {
		t.Errorf("packet = %+v", ins.Spot.Packet)
	}
}

func TestHandleDecodeReplyToOtherStops(t *testing.T) {
	s, ch := newTestSeq(4)
	s.frequency = 14074000
	s.current = "CO8LY" // we are working CO8LY
	// CO8LY answers someone else (not us): we should delete it.
	s.handleDecode(&wsjtx.DecodeMsg{Mode: wsjtx.ModeFT8, Message: "K1ABC CO8LY -05"})
	cmd := <-ch
	del, ok := cmd.(db.DeleteCmd)
	if !ok {
		t.Fatalf("got %T, want DeleteCmd", cmd)
	}
	if del.Call != "CO8LY" || del.Band != 20 {
		t.Errorf("delete = %+v, want CO8LY/20", del)
	}
}

func TestHandleDecodeReplyToUsIgnored(t *testing.T) {
	s, ch := newTestSeq(4)
	s.frequency = 14074000
	s.current = "CO8LY"
	// CO8LY answers US: that's our QSO progressing, do nothing.
	s.handleDecode(&wsjtx.DecodeMsg{Mode: wsjtx.ModeFT8, Message: "W6BSD CO8LY -05"})
	select {
	case cmd := <-ch:
		t.Fatalf("unexpected command %T", cmd)
	default:
	}
}

func TestHandleDecodeRR73Disabled(t *testing.T) {
	s, ch := newTestSeq(4)
	s.frequency = 14074000
	s.considerRR73 = false
	// A third-party RR73 must be ignored while the option is off.
	s.handleDecode(&wsjtx.DecodeMsg{Mode: wsjtx.ModeFT8, Message: "K1ABC CO8LY RR73"})
	select {
	case cmd := <-ch:
		t.Fatalf("unexpected command %T with consider_rr73 off", cmd)
	default:
	}
}

func TestHandleDecodeRR73EnrolsThirdParty(t *testing.T) {
	s, ch := newTestSeq(4)
	s.frequency = 14074000
	s.considerRR73 = true
	for _, msg := range []string{"K1ABC CO8LY RR73", "K1ABC W1AW 73"} {
		s.handleDecode(&wsjtx.DecodeMsg{Mode: wsjtx.ModeFT8, Message: msg})
		cmd := <-ch
		ins, ok := cmd.(db.InsertCmd)
		if !ok {
			t.Fatalf("%q: got %T, want InsertCmd", msg, cmd)
		}
		want := strings.Fields(msg)[1] // the transmitting station
		if ins.Spot.Call != want || ins.Spot.Band != 20 || ins.Spot.Grid != "" {
			t.Errorf("%q: got %+v, want call=%s band=20 grid=empty", msg, ins.Spot, want)
		}
	}
}

func TestHandleDecodeRR73ToUsNotEnrolled(t *testing.T) {
	s, ch := newTestSeq(4)
	s.frequency = 14074000
	s.considerRR73 = true
	// RR73 directed at us completes our own QSO; it is not a new candidate.
	s.handleDecode(&wsjtx.DecodeMsg{Mode: wsjtx.ModeFT8, Message: "W6BSD CO8LY RR73"})
	select {
	case cmd := <-ch:
		t.Fatalf("unexpected command %T for RR73 to us", cmd)
	default:
	}
}

func TestHandleDecodeRR73FromCurrentStops(t *testing.T) {
	s, ch := newTestSeq(4)
	s.frequency = 14074000
	s.considerRR73 = true
	s.current = "CO8LY" // we are working CO8LY
	// CO8LY RR73s a third party: stop and forget it, never enrol it.
	s.handleDecode(&wsjtx.DecodeMsg{Mode: wsjtx.ModeFT8, Message: "K1ABC CO8LY RR73"})
	cmd := <-ch
	if _, ok := cmd.(db.DeleteCmd); !ok {
		t.Fatalf("got %T, want DeleteCmd (stop working the station)", cmd)
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected second command %T (should not also enrol)", extra)
	default:
	}
}

func TestLogCallCountsSessionQSOs(t *testing.T) {
	s, ch := newTestSeq(4)
	for i := 1; i <= 2; i++ {
		s.logCall(&wsjtx.QSOLogged{DXCall: "CO8LY", DialFrequency: 14074000, Mode: "FT8"})
		if _, ok := (<-ch).(db.StatusCmd); !ok {
			t.Fatalf("logCall %d: expected a StatusCmd", i)
		}
		if s.sessionQSOs != i {
			t.Errorf("after %d QSOs, sessionQSOs = %d", i, s.sessionQSOs)
		}
	}
	// The count is surfaced through the published Status snapshot.
	s.publishStatus()
	if got := s.Status().SessionQSOs; got != 2 {
		t.Errorf("Status().SessionQSOs = %d, want 2", got)
	}
}

func TestHandleStatusFT2SetsSubSecondPeriod(t *testing.T) {
	s, _ := newTestSeq(4)
	s.handleStatus(&wsjtx.Status{TXMode: "FT2", Frequency: 50313000})
	if s.period != 3750*time.Millisecond {
		t.Errorf("FT2 period = %v, want 3.75s", s.period)
	}
	// Switching back to a whole-second mode restores the integer-second model.
	s.handleStatus(&wsjtx.Status{TXMode: "FT8", Frequency: 14074000})
	if s.period != 0 {
		t.Errorf("FT8 should clear sub-second period, got %v", s.period)
	}
	if !s.sequence[2] {
		t.Error("FT8 whole-second sequence not restored")
	}
}

func TestNewSequenceFT2FiresOncePerPeriod(t *testing.T) {
	s, _ := newTestSeq(0)
	s.period = 3750 * time.Millisecond
	// A minute boundary is also a 3.75 s period boundary (60000/3750 = 16).
	base := time.Date(2026, 6, 19, 20, 0, 0, 0, time.UTC)
	orig := nowFunc
	defer func() { nowFunc = orig }()

	nowFunc = func() time.Time { return base }
	if !s.newSequence() {
		t.Fatal("first check of a period should start a sequence")
	}
	nowFunc = func() time.Time { return base.Add(500 * time.Millisecond) }
	if s.newSequence() {
		t.Error("a second check within the same 3.75s period must not re-fire")
	}
	nowFunc = func() time.Time { return base.Add(3750 * time.Millisecond) }
	if !s.newSequence() {
		t.Error("the next 3.75s period should start a new sequence")
	}
}

func TestHandleStatusUpdatesState(t *testing.T) {
	s, ch := newTestSeq(4)
	s.handleStatus(&wsjtx.Status{
		Frequency:    14074000,
		TXMode:       "FT8",
		Transmitting: true,
		TXEnabled:    true,
		DXCall:       "CO8LY",
		TxMessage:    "CO8LY W6BSD -10",
	})
	if s.frequency != 14074000 {
		t.Errorf("frequency = %d", s.frequency)
	}
	if !s.txStatus {
		t.Error("txStatus should be true")
	}
	if !s.sequence[2] {
		t.Error("sequence not set to FT8")
	}
	cmd := <-ch
	st, ok := cmd.(db.StatusCmd)
	if !ok || st.Call != "CO8LY" || st.Status != 1 || st.Band != 20 {
		t.Errorf("got %+v, want StatusCmd CO8LY/20/1", cmd)
	}
}

// fakeSelector always returns the same candidate.
type fakeSelector struct {
	name string
	cand selector.Candidate
}

func (f fakeSelector) Name() string                       { return f.name }
func (f fakeSelector) Get(int) (selector.Candidate, bool) { return f.cand, true }

func TestSequenceCheckSelectsStation(t *testing.T) {
	s, _ := newTestSeq(4)
	s.frequency = 14074000
	s.txStatus = false
	s.sequence = map[int]bool{5: true}

	cand := selector.Candidate{}
	cand.Call = "CO8LY"
	setChain(s, selector.Chain{fakeSelector{name: "Any", cand: cand}})

	// Pin the clock to a second that is in the sequence set.
	orig := nowFunc
	nowFunc = func() time.Time {
		return time.Date(2026, 6, 18, 12, 0, 5, 0, time.UTC)
	}
	defer func() { nowFunc = orig }()

	s.tracker.retries = 9
	s.sequenceCheck()
	if s.current != "CO8LY" {
		t.Errorf("current = %q, want CO8LY", s.current)
	}
	if s.tracker.retries != 0 {
		t.Errorf("tracker not reset: retries = %d", s.tracker.retries)
	}
	// A second call within the same second must be a no-op (lastSecond guard).
	s.current = ""
	s.sequenceCheck()
	if s.current != "" {
		t.Error("sequenceCheck re-fired within the same second")
	}
}

func TestReloadAppliesNewSettings(t *testing.T) {
	s, _ := newTestSeq(4)
	s.reload = make(chan reloadParams, 1)
	s.txPower = 30
	s.followFreq = false
	s.tracker.max = 5
	oldChain := selector.Chain{fakeSelector{name: "Any"}}
	setChain(s, oldChain)

	newChain := selector.Chain{fakeSelector{name: "DXCC100"}}
	s.Reload(config.FT8Ctrl{TXPower: 100, FollowFrequency: true, TXRetries: 3}, newChain)

	// Nothing applied until the Run loop drains the channel.
	if s.txPower != 30 {
		t.Fatal("reload applied before applyReload")
	}
	s.applyReload()
	if s.txPower != 100 {
		t.Errorf("txPower = %d, want 100", s.txPower)
	}
	if !s.followFreq {
		t.Error("followFreq not updated")
	}
	if s.tracker.max != 3 {
		t.Errorf("tracker.max = %d, want 3", s.tracker.max)
	}
	if got := (*s.chain.Load())[0].Name(); got != "DXCC100" {
		t.Errorf("chain not swapped: %q", got)
	}
}

func TestPickReturnsChainSelection(t *testing.T) {
	s, _ := newTestSeq(0)
	cand := selector.Candidate{}
	cand.Call = "CO8LY"
	setChain(s, selector.Chain{fakeSelector{name: "DXCC100", cand: cand}})

	sel, ok := s.Pick(20)
	if !ok {
		t.Fatal("Pick returned ok=false, want a selection")
	}
	if sel.Call != "CO8LY" || sel.Selector != "DXCC100" {
		t.Errorf("Pick = %q via %q, want CO8LY via DXCC100", sel.Call, sel.Selector)
	}

	// An empty chain declines.
	setChain(s, selector.Chain{})
	if _, ok := s.Pick(20); ok {
		t.Error("empty chain should decline")
	}
}

func TestReloadReplacesPending(t *testing.T) {
	s, _ := newTestSeq(4)
	s.reload = make(chan reloadParams, 1)
	// Two reloads before any applyReload: the latest must win.
	s.Reload(config.FT8Ctrl{TXPower: 50, TXRetries: 5}, nil)
	s.Reload(config.FT8Ctrl{TXPower: 99, TXRetries: 2}, nil)
	s.applyReload()
	if s.txPower != 99 || s.tracker.max != 2 {
		t.Errorf("latest reload did not win: txPower=%d max=%d", s.txPower, s.tracker.max)
	}
}

func TestSequenceCheckSkipsWhileTransmitting(t *testing.T) {
	s, _ := newTestSeq(4)
	s.txStatus = true
	s.sequence = map[int]bool{5: true}
	setChain(s, selector.Chain{fakeSelector{name: "Any"}})
	orig := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 6, 18, 12, 0, 5, 0, time.UTC) }
	defer func() { nowFunc = orig }()

	s.sequenceCheck()
	if s.current != "" {
		t.Error("should not select a station while transmitting")
	}
}
