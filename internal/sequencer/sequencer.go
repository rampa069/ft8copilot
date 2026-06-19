// Package sequencer is the heart of the daemon: it listens to WSJT-X UDP
// packets, records CQ-calling stations into the database, and automatically
// replies to the station the selector chain judges most likely to complete a
// QSO. Port of the Sequencer class in ft8ctrl.py.
package sequencer

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/selector"
	"github.com/rampa069/ft8copilot/internal/wsjtx"
)

// readTimeout bounds each UDP read so the transmit-sequence check runs roughly
// every readTimeout even when no packets arrive (the select(...,.7) in the
// original).
const readTimeout = 700 * time.Millisecond

// nowFunc is the clock, overridable in tests.
var nowFunc = time.Now

// Sequencer drives WSJT-X over UDP.
type Sequencer struct {
	conn         *net.UDPConn
	mycall       string
	mygrid       string // 4-char Maidenhead grid for our own CQ
	followFreq   bool
	considerRR73 bool // also enrol stations sending RR73/73 as candidates
	txPower      int
	// chain is the live selector chain. The Run loop is its only writer (via
	// applyReload), but Pick reads it from the TUI goroutine, so it is held in
	// an atomic.Pointer to keep that read race-free across hot reloads.
	chain atomic.Pointer[selector.Chain]
	cmds  chan<- db.Command
	log   *slog.Logger

	loggerConn *net.UDPConn // optional secondary logger forward

	// reload carries hot-reloaded settings from the SIGHUP handler. It is
	// buffered (size 1) and drained at the top of each Run iteration, so the
	// single-threaded Run loop is the only mutator of the fields above — no
	// locking required.
	reload chan reloadParams

	// paused gates the autopilot: while set, no station is called at sequence
	// boundaries, but packet ingestion (decodes, status) keeps filling the
	// database. It is toggled from another goroutine (the TUI), so it is atomic.
	paused atomic.Bool

	// cqRequest is a one-shot "call CQ now" trigger set from the TUI and consumed
	// by the Run loop, so the actual transmit happens in the loop's goroutine
	// (which owns peer/conn). The CQ-mode state machine (FT8CoPilot-3ef) will
	// later drive CQ calls itself; this is the manual primitive.
	cqRequest atomic.Bool

	// runtime state
	peer        *net.UDPAddr
	frequency   uint64
	txStatus    bool
	current     string // callsign we are currently trying to work
	sequence    map[int]bool
	tracker     txTracker
	lastSecond  int
	sessionQSOs int // QSOs logged since startup (Run loop owns it, like current)

	// status is a snapshot the Run loop publishes for other goroutines (the TUI)
	// to read without locking the runtime fields above.
	status atomic.Pointer[Status]
}

// Status is a point-in-time view of the sequencer for the TUI status panel.
type Status struct {
	Frequency    uint64 // dial frequency in Hz
	Band         int    // band in metres
	Transmitting bool   // currently keying the transmitter
	Paused       bool   // autopilot suspended
	Current      string // callsign being worked, empty when idle
	SessionQSOs  int    // QSOs logged since startup
}

// Status returns the most recent published snapshot (zero value before the Run
// loop has run once). The Paused field always reflects the live flag, so a
// pause/resume is visible immediately without waiting for the next publish. Safe
// to call from any goroutine.
func (s *Sequencer) Status() Status {
	var st Status
	if p := s.status.Load(); p != nil {
		st = *p
	}
	st.Paused = s.paused.Load()
	return st
}

// Pick reports the candidate the live selector chain would call next on a band,
// or false when the chain declines. It runs the same Chain.Select the Run loop
// uses, so the TUI can highlight exactly the station the autopilot will work.
// Safe to call from any goroutine: the chain is read atomically and Select only
// queries the (mutex-guarded, cached) candidate pool. It does not consider the
// paused flag or in-flight QSO — it answers "who is next", not "are we calling".
func (s *Sequencer) Pick(band int) (selector.Selection, bool) {
	c := s.chain.Load()
	if c == nil {
		return selector.Selection{}, false
	}
	return c.Select(band)
}

// publishStatus snapshots the runtime fields for readers. Called from the Run
// loop only, so reads of the runtime fields are race-free.
func (s *Sequencer) publishStatus() {
	s.status.Store(&Status{
		Frequency:    s.frequency,
		Band:         db.Band(s.frequency),
		Transmitting: s.txStatus,
		Paused:       s.paused.Load(),
		Current:      s.current,
		SessionQSOs:  s.sessionQSOs,
	})
}

// New creates a Sequencer, binding the WSJT-X UDP socket. cmds is the channel
// consumed by the db.Writer; chain is the configured selector chain.
func New(cfg config.FT8Ctrl, chain selector.Chain, cmds chan<- db.Command, log *slog.Logger) (*Sequencer, error) {
	if log == nil {
		log = slog.Default()
	}
	ip := net.ParseIP(cfg.WSJTIP)
	if ip == nil {
		// Allow hostnames too.
		addrs, err := net.LookupIP(cfg.WSJTIP)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("sequencer: resolve wsjt_ip %q: %w", cfg.WSJTIP, err)
		}
		ip = addrs[0]
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: cfg.WSJTPort})
	if err != nil {
		return nil, fmt.Errorf("sequencer: bind %s:%d: %w", cfg.WSJTIP, cfg.WSJTPort, err)
	}

	s := &Sequencer{
		conn:         conn,
		mycall:       cfg.MyCall,
		mygrid:       grid4(cfg.MyGrid),
		followFreq:   cfg.FollowFrequency,
		considerRR73: cfg.ConsiderRR73,
		txPower:      cfg.TXPower,
		cmds:         cmds,
		log:          log,
		reload:       make(chan reloadParams, 1),
		sequence:     map[int]bool{},
		tracker:      txTracker{max: cfg.TXRetries},
		lastSecond:   -1,
	}
	s.chain.Store(&chain)

	if cfg.LoggerIP != "" && cfg.LoggerPort != 0 {
		laddr := &net.UDPAddr{IP: net.ParseIP(cfg.LoggerIP), Port: cfg.LoggerPort}
		if lc, err := net.DialUDP("udp", nil, laddr); err == nil {
			s.loggerConn = lc
		} else {
			log.Warn("secondary logger disabled", "err", err)
		}
	}
	return s, nil
}

// reloadParams holds the subset of configuration that can be applied to a
// running Sequencer without reopening sockets or the database.
type reloadParams struct {
	chain        selector.Chain
	txPower      int
	followFreq   bool
	considerRR73 bool
	txRetries    int
}

// Reload hands new hot-reloadable settings to the running Sequencer. It is
// non-blocking: the values are picked up at the next Run iteration. A pending
// reload that has not been consumed yet is replaced. Reload must be called from
// a single goroutine (the SIGHUP handler).
func (s *Sequencer) Reload(cfg config.FT8Ctrl, chain selector.Chain) {
	p := reloadParams{
		chain:        chain,
		txPower:      cfg.TXPower,
		followFreq:   cfg.FollowFrequency,
		considerRR73: cfg.ConsiderRR73,
		txRetries:    cfg.TXRetries,
	}
	// Discard any unconsumed reload so we always apply the latest.
	select {
	case <-s.reload:
	default:
	}
	s.reload <- p
}

// applyReload folds in a pending reload, if any. Called only from Run, so it is
// the sole mutator of the affected fields.
func (s *Sequencer) applyReload() {
	select {
	case p := <-s.reload:
		s.chain.Store(&p.chain)
		s.txPower = p.txPower
		s.followFreq = p.followFreq
		s.considerRR73 = p.considerRR73
		s.tracker.max = p.txRetries
		s.log.Info("sequencer reloaded", "tx_retries", p.txRetries,
			"tx_power", p.txPower, "follow_frequency", p.followFreq,
			"consider_rr73", p.considerRR73)
	default:
	}
}

// Close releases the sockets.
func (s *Sequencer) Close() error {
	if s.loggerConn != nil {
		_ = s.loggerConn.Close()
	}
	return s.conn.Close()
}

// Run processes packets until the context is cancelled.
func (s *Sequencer) Run(ctx context.Context) error {
	s.log.Info("ft8ctrl running", "addr", s.conn.LocalAddr())
	buf := make([]byte, 1024)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.applyReload()
		if s.cqRequest.CompareAndSwap(true, false) {
			s.callCQ()
		}
		_ = s.conn.SetReadDeadline(nowFunc().Add(readTimeout))
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			ne, ok := err.(net.Error)
			isTimeout := ok && ne.Timeout()
			// On timeout there is simply no packet this tick; fall through to
			// the sequence check. Other errors are fatal if the context is
			// cancelled, otherwise logged and retried.
			if !isTimeout {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				s.log.Error("udp read", "err", err)
			}
		} else {
			s.peer = addr
			s.handlePacket(buf[:n])
		}
		s.sequenceCheck()
		s.publishStatus()
	}
}

func (s *Sequencer) handlePacket(raw []byte) {
	msg, err := wsjtx.Decode(raw)
	if err != nil {
		s.log.Debug("undecoded packet", "err", err)
		return
	}
	switch p := msg.(type) {
	case *wsjtx.Heartbeat, *wsjtx.ADIF:
		// ignored
	case *wsjtx.QSOLogged:
		s.logCall(p)
		s.current = ""
	case *wsjtx.DecodeMsg:
		s.handleDecode(p)
	case *wsjtx.Status:
		s.handleStatus(p)
	default:
		s.log.Debug("unhandled packet", "type", msg.Type())
	}
}

func (s *Sequencer) handleDecode(p *wsjtx.DecodeMsg) {
	m := parseMessage(p.Message)
	band := db.Band(s.frequency)
	switch m.kind {
	case msgReply:
		// The station we are working answered someone else: stop and forget it.
		if m.call == s.current && m.to != s.mycall {
			s.log.Info("stop transmit: station replying to someone else",
				"call", m.call, "to", m.to)
			s.stopTransmit()
			s.send(db.DeleteCmd{Call: m.call, Band: band})
			return
		}
		// consider_rr73: a station signing off a QSO with a third party is about
		// to be free — enrol it as a candidate (no grid, like a broken CQ).
		if s.considerRR73 && m.rr73 && m.to != s.mycall {
			s.log.Info("enrolling RR73/73 station", "call", m.call, "to", m.to)
			s.send(db.InsertCmd{Spot: db.Spot{
				Call:      m.call,
				Frequency: s.frequency,
				Band:      band,
				Packet: db.Packet{
					New:            p.New,
					Time:           p.Time,
					SNR:            p.SNR,
					DeltaTime:      p.DeltaTime,
					DeltaFrequency: p.DeltaFrequency,
					Mode:           string(p.Mode),
					Message:        p.Message,
					LowConfidence:  p.LowConfidence,
					OffAir:         p.OffAir,
				},
			}})
		}
	case msgCQ:
		s.send(db.InsertCmd{Spot: db.Spot{
			Call:      m.call,
			Extra:     m.extra,
			Grid:      m.grid,
			Frequency: s.frequency,
			Band:      band,
			Packet: db.Packet{
				New:            p.New,
				Time:           p.Time,
				SNR:            p.SNR,
				DeltaTime:      p.DeltaTime,
				DeltaFrequency: p.DeltaFrequency,
				Mode:           string(p.Mode),
				Message:        p.Message,
				LowConfidence:  p.LowConfidence,
				OffAir:         p.OffAir,
			},
		}})
	}
}

func (s *Sequencer) handleStatus(p *wsjtx.Status) {
	// WSJT-X may emit several Status packets with Transmitting=true for one
	// transmission; gating on Decoding avoids inflating the retry count.
	if s.tracker.observe(p.Decoding, p.Transmitting, p.TxMessage) {
		s.log.Info("retries exceeded, stopping transmit")
		s.stopTransmit()
		return
	}

	if seq, ok := sequenceSeconds[p.TXMode]; ok {
		s.sequence = seq
	}
	s.frequency = p.Frequency
	s.txStatus = p.Transmitting || p.TXEnabled

	if p.Transmitting && p.DXCall != "" {
		s.send(db.StatusCmd{Call: p.DXCall, Band: db.Band(s.frequency), Status: 1})
	}
}

// Pause suspends the autopilot: new stations are no longer called at sequence
// boundaries. Database ingestion is unaffected. Safe to call from any goroutine.
func (s *Sequencer) Pause() {
	if !s.paused.Swap(true) {
		s.log.Info("autopilot paused")
	}
}

// Resume re-enables calling stations at sequence boundaries.
func (s *Sequencer) Resume() {
	if s.paused.Swap(false) {
		s.log.Info("autopilot resumed")
	}
}

// TogglePause flips the paused state and returns the new value.
func (s *Sequencer) TogglePause() bool {
	if s.Paused() {
		s.Resume()
		return false
	}
	s.Pause()
	return true
}

// Paused reports whether the autopilot is currently suspended.
func (s *Sequencer) Paused() bool { return s.paused.Load() }

// sequenceCheck calls the best available station at a sequence boundary, when we
// are not already transmitting.
func (s *Sequencer) sequenceCheck() {
	if s.txStatus {
		return
	}
	// While paused, ingest packets but never initiate a call.
	if s.paused.Load() {
		return
	}
	now := nowFunc().UTC()
	sec := now.Second()
	if !s.sequence[sec] {
		return
	}
	// Avoid re-firing within the same second (the original slept 1s here).
	if sec == s.lastSecond {
		return
	}
	s.lastSecond = sec

	sel, ok := s.chain.Load().Select(db.Band(s.frequency))
	if !ok {
		s.current = ""
		return
	}
	s.callStation(sel)
	s.current = sel.Call
	s.tracker.reset()
}

// callStation transmits a reply to the selected station.
func (s *Sequencer) callStation(sel selector.Selection) {
	if s.peer == nil {
		return
	}
	s.log.Info("calling",
		"call", sel.Call, "country", sel.Country, "snr", sel.SNR,
		"distance", int(sel.Distance), "band", sel.Band, "selector", sel.Selector)

	reply := &wsjtx.Reply{
		Time:           sel.Time,
		SNR:            sel.SNR,
		DeltaTime:      sel.Packet.DeltaTime,
		DeltaFrequency: sel.Packet.DeltaFrequency,
		Mode:           wsjtx.Mode(sel.Packet.Mode),
		Message:        sel.Packet.Message,
	}
	if s.followFreq {
		reply.Modifiers = wsjtx.ShiftMod
	}
	if _, err := s.conn.WriteToUDP(reply.Encode(), s.peer); err != nil {
		s.log.Error("transmit reply", "err", err)
	}
}

// RequestCQ asks the Run loop to call CQ on its next iteration. Safe to call
// from any goroutine; the transmit itself runs in the Run loop (which owns the
// peer/conn). It is a one-shot — a pending request that has not fired yet is
// not duplicated.
func (s *Sequencer) RequestCQ() { s.cqRequest.Store(true) }

// callCQ transmits a CQ with our callsign and grid via WSJT-X free text.
//
// WSJT-X has no dedicated "call CQ" UDP command, so we use a FreeText message
// ("CQ <call> <grid>", Send=true). For this to actually key the radio, WSJT-X
// must be ready to transmit: a running instance, the rig connected, and Tx
// enabled (the free text replaces the current Tx message and transmits it on
// the next slot). It transmits ONCE; repeated/periodic CQ is the job of the
// CQ-mode state machine (FT8CoPilot-3ef). Called from the Run loop only.
func (s *Sequencer) callCQ() {
	if s.peer == nil {
		s.log.Warn("call CQ requested but no WSJT-X peer yet")
		return
	}
	msg := s.cqMessage()
	s.log.Info("calling CQ", "message", msg)
	if _, err := s.conn.WriteToUDP(wsjtx.NewFreeText(msg).Encode(), s.peer); err != nil {
		s.log.Error("transmit CQ", "err", err)
	}
}

// cqMessage builds the free-text CQ ("CQ <call> <grid>", or "CQ <call>" when no
// grid is configured).
func (s *Sequencer) cqMessage() string {
	if s.mygrid == "" {
		return "CQ " + s.mycall
	}
	return "CQ " + s.mycall + " " + s.mygrid
}

// grid4 trims a Maidenhead locator to its 4-character field/square, which is all
// a standard FT8 CQ carries.
func grid4(g string) string {
	g = strings.TrimSpace(g)
	if len(g) > 4 {
		return g[:4]
	}
	return g
}

// stopTransmit asks WSJT-X to halt transmission immediately.
func (s *Sequencer) stopTransmit() {
	if s.peer == nil {
		return
	}
	if _, err := s.conn.WriteToUDP((&wsjtx.HaltTx{}).Encode(), s.peer); err != nil {
		s.log.Error("halt tx", "err", err)
	}
}

// logCall records a logged QSO: forwards it to the optional secondary logger and
// marks the station worked in the database.
func (s *Sequencer) logCall(p *wsjtx.QSOLogged) {
	s.forwardToLogger(p)
	s.send(db.StatusCmd{Call: p.DXCall, Band: db.Band(p.DialFrequency), Status: 2})
	s.sessionQSOs++
	s.log.Info("logged call", "call", p.DXCall, "grid", p.DXGrid, "mode", p.Mode,
		"session_qsos", s.sessionQSOs)
}

// forwardToLogger re-sends a logged QSO to a secondary logging application,
// stamping TX power and tagging the comment. Port of sendto_log in ft8ctrl.py.
func (s *Sequencer) forwardToLogger(p *wsjtx.QSOLogged) {
	if s.loggerConn == nil {
		return
	}
	logged := *p
	if s.txPower != 0 {
		logged.TXPower = fmt.Sprintf("%d", s.txPower)
	}
	logged.Comments = "[ft8ctrl] " + p.Comments
	if _, err := s.loggerConn.Write(logged.Encode()); err != nil {
		s.log.Error("forward to logger", "err", err)
	}
}

func (s *Sequencer) send(cmd db.Command) {
	select {
	case s.cmds <- cmd:
	default:
		// Drop rather than block the UDP loop if the writer is backed up.
		s.log.Warn("db command channel full, dropping", "type", fmt.Sprintf("%T", cmd))
	}
}
