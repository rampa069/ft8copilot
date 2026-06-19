package wsjtx

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotWSJTX is returned by Decode when the magic number does not match.
var ErrNotWSJTX = errors.New("wsjtx: not a WSJT-X packet (bad magic number)")

// UnknownTypeError is returned by Decode for a well-formed packet whose type is
// not handled by this package.
type UnknownTypeError struct{ Type PacketType }

func (e *UnknownTypeError) Error() string {
	return fmt.Sprintf("wsjtx: unknown packet type %d", e.Type)
}

// Message is implemented by every decoded or encodable WSJT-X packet.
type Message interface {
	Type() PacketType
}

// Encodable is a Message that can be serialized for transmission to WSJT-X.
type Encodable interface {
	Message
	Encode() []byte
}

// Header carries the fields common to every packet. It is embedded in decoded
// messages so the originating client id is available.
type Header struct {
	ClientID string
}

func writeHeader(w *writer, t PacketType, clientID string) {
	w.uint32(Magic)
	w.uint32(Schema)
	w.uint32(uint32(t))
	w.str(clientID)
}

// Decode parses a raw datagram into a concrete Message. It returns ErrNotWSJTX
// if the magic number is wrong and *UnknownTypeError for unhandled packet
// types, mirroring ft8_decode in the original.
func Decode(raw []byte) (Message, error) {
	r := newReader(raw)
	magic := r.uint32()
	if r.readErr() != nil {
		return nil, ErrNotWSJTX
	}
	if magic != Magic {
		return nil, ErrNotWSJTX
	}
	_ = r.uint32() // schema version
	t := PacketType(r.uint32())
	clientID := r.str()
	h := Header{ClientID: clientID}

	var m Message
	switch t {
	case TypeHeartbeat:
		m = decodeHeartbeat(r, h)
	case TypeStatus:
		m = decodeStatus(r, h)
	case TypeDecode:
		m = decodeDecode(r, h)
	case TypeClear:
		m = decodeClear(r, h)
	case TypeQSOLogged:
		m = decodeQSOLogged(r, h)
	case TypeClose:
		m = &Close{Header: h}
	case TypeLoggedADIF:
		m = decodeADIF(r, h)
	default:
		return nil, &UnknownTypeError{Type: t}
	}
	if err := r.readErr(); err != nil {
		return nil, err
	}
	return m, nil
}

// ---- Heartbeat (type 0) ----

// Heartbeat is the keep-alive packet exchanged between WSJT-X and clients.
type Heartbeat struct {
	Header
	MaxSchema uint32
	Version   string
	Revision  string
}

// Type returns the packet type.
func (*Heartbeat) Type() PacketType { return TypeHeartbeat }

// NewHeartbeat returns a Heartbeat populated with this client's defaults.
func NewHeartbeat() *Heartbeat {
	return &Heartbeat{MaxSchema: Schema, Version: Version, Revision: Revision}
}

func decodeHeartbeat(r *reader, h Header) *Heartbeat {
	return &Heartbeat{
		Header:    h,
		MaxSchema: r.uint32(),
		Version:   r.str(),
		Revision:  r.str(),
	}
}

// Encode serialises the message into the WSJT-X wire format.
func (p *Heartbeat) Encode() []byte {
	w := &writer{}
	clientID := p.ClientID
	if clientID == "" {
		clientID = ClientID
	}
	writeHeader(w, TypeHeartbeat, clientID)
	w.uint32(p.MaxSchema)
	w.str(p.Version)
	w.str(p.Revision)
	return w.bytes()
}

// ---- Status (type 1) ----

// Status reports WSJT-X's current operating state (frequency, mode, calls, etc.).
type Status struct {
	Header
	Frequency     uint64
	Mode          string
	DXCall        string
	Report        string
	TXMode        string
	TXEnabled     bool
	Transmitting  bool
	Decoding      bool
	RXdf          uint32
	TXdf          uint32
	DeCall        string
	DeGrid        string
	DXGrid        string
	TXWatchdog    bool
	SubMode       string
	Fastmode      bool
	SOMode        SOMode
	FreqTolerance uint32
	TRPeriod      uint32
	ConfigName    string
	TxMessage     string
}

// Type returns the packet type.
func (*Status) Type() PacketType { return TypeStatus }

func decodeStatus(r *reader, h Header) *Status {
	return &Status{
		Header:        h,
		Frequency:     r.uint64(),
		Mode:          r.str(),
		DXCall:        r.str(),
		Report:        r.str(),
		TXMode:        r.str(),
		TXEnabled:     r.boolVal(),
		Transmitting:  r.boolVal(),
		Decoding:      r.boolVal(),
		RXdf:          r.uint32(),
		TXdf:          r.uint32(),
		DeCall:        r.str(),
		DeGrid:        r.str(),
		DXGrid:        r.str(),
		TXWatchdog:    r.boolVal(),
		SubMode:       r.str(),
		Fastmode:      r.boolVal(),
		SOMode:        SOMode(r.byteVal()),
		FreqTolerance: r.uint32(),
		TRPeriod:      r.uint32(),
		ConfigName:    r.str(),
		TxMessage:     r.str(),
	}
}

// ---- Decode (type 2) ----

// DecodeMsg reports a single decoded transmission heard by WSJT-X.
type DecodeMsg struct {
	Header
	New            bool
	Time           time.Time
	SNR            int32
	DeltaTime      float64
	DeltaFrequency uint32
	Mode           Mode
	Message        string
	LowConfidence  bool
	OffAir         bool
}

// Type returns the packet type.
func (*DecodeMsg) Type() PacketType { return TypeDecode }

func decodeDecode(r *reader, h Header) *DecodeMsg {
	return &DecodeMsg{
		Header:         h,
		New:            r.boolVal(),
		Time:           wsTimeToTime(r.uint32()),
		SNR:            r.int32(),
		DeltaTime:      round3(r.float64()),
		DeltaFrequency: r.uint32(),
		Mode:           Mode(r.str()),
		Message:        r.str(),
		LowConfidence:  r.boolVal(),
		OffAir:         r.boolVal(),
	}
}

func round3(v float64) float64 {
	return float64(int64(v*1000+sign(v)*0.5)) / 1000
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// ---- Clear (type 3) ----

// Clear requests that WSJT-X clear its decode window(s).
type Clear struct {
	Header
	HasWindow bool
	Window    byte
}

// Type returns the packet type.
func (*Clear) Type() PacketType { return TypeClear }

func decodeClear(r *reader, h Header) *Clear {
	c := &Clear{Header: h}
	if r.remaining() > 0 {
		c.HasWindow = true
		c.Window = r.byteVal()
	}
	return c
}

// ---- Reply (type 4) ----

// Reply instructs WSJT-X to reply to a previously decoded transmission.
type Reply struct {
	Header
	Time           time.Time
	SNR            int32
	DeltaTime      float64
	DeltaFrequency uint32
	Mode           Mode
	Message        string
	LowConfidence  bool
	Modifiers      Modifier
}

// Type returns the packet type.
func (*Reply) Type() PacketType { return TypeReply }

// Encode serialises the message into the WSJT-X wire format.
func (p *Reply) Encode() []byte {
	w := &writer{}
	clientID := p.ClientID
	if clientID == "" {
		clientID = replyClientID
	}
	writeHeader(w, TypeReply, clientID)
	w.uint32(timeToWSTime(p.Time))
	w.int32(p.SNR)
	w.float64(p.DeltaTime)
	w.uint32(p.DeltaFrequency)
	w.str(string(p.Mode))
	w.str(p.Message)
	w.boolVal(p.LowConfidence)
	w.byteVal(byte(p.Modifiers))
	return w.bytes()
}

// ---- QSO Logged (type 5) ----

// QSOLogged reports a QSO that WSJT-X has just logged.
type QSOLogged struct {
	Header
	DateTimeOff    time.Time
	DXCall         string
	DXGrid         string
	DialFrequency  uint64
	Mode           string
	ReportSent     string
	ReportReceived string
	TXPower        string
	Comments       string
	Name           string
	DateTimeOn     time.Time
	OpCall         string
	MyCall         string
	MyGrid         string
	ExSent         string
	ExReceived     string
	PropMode       string
}

// Type returns the packet type.
func (*QSOLogged) Type() PacketType { return TypeQSOLogged }

func decodeQSOLogged(r *reader, h Header) *QSOLogged {
	return &QSOLogged{
		Header:         h,
		DateTimeOff:    r.dateTime(),
		DXCall:         r.str(),
		DXGrid:         r.str(),
		DialFrequency:  r.uint64(),
		Mode:           r.str(),
		ReportSent:     r.str(),
		ReportReceived: r.str(),
		TXPower:        r.str(),
		Comments:       r.str(),
		Name:           r.str(),
		DateTimeOn:     r.dateTime(),
		OpCall:         r.str(),
		MyCall:         r.str(),
		MyGrid:         r.str(),
		ExSent:         r.str(),
		ExReceived:     r.str(),
		PropMode:       r.str(),
	}
}

// Encode serialises the message into the WSJT-X wire format.
func (p *QSOLogged) Encode() []byte {
	w := &writer{}
	clientID := p.ClientID
	if clientID == "" {
		clientID = ClientID
	}
	writeHeader(w, TypeQSOLogged, clientID)
	w.dateTime(p.DateTimeOff)
	w.str(p.DXCall)
	w.str(p.DXGrid)
	w.uint64(p.DialFrequency)
	w.str(p.Mode)
	w.str(p.ReportSent)
	w.str(p.ReportReceived)
	w.str(p.TXPower)
	w.str(p.Comments)
	w.str(p.Name)
	w.dateTime(p.DateTimeOn)
	w.str(p.OpCall)
	w.str(p.MyCall)
	w.str(p.MyGrid)
	w.str(p.ExSent)
	w.str(p.ExReceived)
	w.str(p.PropMode)
	return w.bytes()
}

// ---- Close (type 6) ----

// Close signals that the sender is shutting down.
type Close struct {
	Header
}

// Type returns the packet type.
func (*Close) Type() PacketType { return TypeClose }

// Encode serialises the message into the WSJT-X wire format.
func (p *Close) Encode() []byte {
	w := &writer{}
	clientID := p.ClientID
	if clientID == "" {
		clientID = ClientID
	}
	writeHeader(w, TypeClose, clientID)
	return w.bytes()
}

// ---- Halt Tx (type 8) ----

// HaltTx stops transmission. Mode=false stops immediately; Mode=true stops at
// the end of the current sequence.
type HaltTx struct {
	Header
	Mode bool
}

// Type returns the packet type.
func (*HaltTx) Type() PacketType { return TypeHaltTx }

// Encode serialises the message into the WSJT-X wire format.
func (p *HaltTx) Encode() []byte {
	w := &writer{}
	clientID := p.ClientID
	if clientID == "" {
		clientID = ClientID
	}
	writeHeader(w, TypeHaltTx, clientID)
	w.boolVal(p.Mode)
	return w.bytes()
}

// ---- Free Text (type 9) ----

// FreeText sets (and optionally transmits) a free-text message in WSJT-X.
type FreeText struct {
	Header
	Text string
	Send bool
}

// Type returns the packet type.
func (*FreeText) Type() PacketType { return TypeFreeText }

// NewFreeText returns a FreeText with Send defaulting to true.
func NewFreeText(text string) *FreeText {
	return &FreeText{Text: text, Send: true}
}

// Encode serialises the message into the WSJT-X wire format.
func (p *FreeText) Encode() []byte {
	w := &writer{}
	clientID := p.ClientID
	if clientID == "" {
		clientID = ClientID
	}
	writeHeader(w, TypeFreeText, clientID)
	w.str(p.Text)
	w.boolVal(p.Send)
	return w.bytes()
}

// ---- Logged ADIF (type 12) ----

// ADIF carries the ADIF record for a QSO that WSJT-X has logged.
type ADIF struct {
	Header
	ADIF string
}

// Type returns the packet type.
func (*ADIF) Type() PacketType { return TypeLoggedADIF }

func decodeADIF(r *reader, h Header) *ADIF {
	return &ADIF{Header: h, ADIF: r.str()}
}

// ---- Highlight Callsign (type 13) ----

// HighlightCallsign instructs WSJT-X to color a callsign in its decode windows.
// Colors are Qt QColor components (alpha-ish leading 0xffff written by Encode).
type HighlightCallsign struct {
	Header
	Call          string
	Foreground    [3]uint16
	Background    [3]uint16
	HighlightLast bool
}

// Type returns the packet type.
func (*HighlightCallsign) Type() PacketType { return TypeHighlightCallsign }

// NewHighlightCallsign returns a highlight packet with the original's default
// white-on-black colors.
func NewHighlightCallsign(call string) *HighlightCallsign {
	return &HighlightCallsign{
		Call:          call,
		Foreground:    [3]uint16{0xffff, 0xff, 0xff},
		Background:    [3]uint16{0, 0, 0},
		HighlightLast: true,
	}
}

// Encode serialises the message into the WSJT-X wire format.
func (p *HighlightCallsign) Encode() []byte {
	w := &writer{}
	clientID := p.ClientID
	if clientID == "" {
		clientID = ClientID
	}
	writeHeader(w, TypeHighlightCallsign, clientID)
	w.str(p.Call)
	w.uint16(0xffff)
	for _, v := range p.Foreground {
		w.uint16(v)
	}
	w.uint16(0xffff)
	for _, v := range p.Background {
		w.uint16(v)
	}
	w.boolVal(p.HighlightLast)
	return w.bytes()
}
