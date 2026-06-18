package wsjtx

// Protocol constants. The WSJT-X UDP messages are serialized with Qt's
// QDataStream in big-endian order. See NetworkMessage.hpp in the WSJT-X source.
const (
	Magic    uint32 = 0xADBCCBDA
	Schema   uint32 = 2
	Version         = "1.1"    // default Heartbeat version
	Revision        = "1a"     // default Heartbeat revision
	ClientID        = "AUTOFS" // default client id used on outgoing packets

	// replyClientID is the client id carried on Reply packets. It differs from
	// ClientID; this matches the original FT8Commander behavior.
	replyClientID = "AUTOFT"
)

// PacketType identifies a WSJT-X message. The numeric values are wire values.
type PacketType uint32

const (
	TypeHeartbeat         PacketType = 0  // Out/In
	TypeStatus            PacketType = 1  // Out
	TypeDecode            PacketType = 2  // Out
	TypeClear             PacketType = 3  // Out/In
	TypeReply             PacketType = 4  // In
	TypeQSOLogged         PacketType = 5  // Out
	TypeClose             PacketType = 6  // Out/In
	TypeReplay            PacketType = 7  // In
	TypeHaltTx            PacketType = 8  // In
	TypeFreeText          PacketType = 9  // In
	TypeWSPRDecode        PacketType = 10 // Out
	TypeLocation          PacketType = 11 // In
	TypeLoggedADIF        PacketType = 12 // Out
	TypeHighlightCallsign PacketType = 13 // In
	TypeSwitchConfig      PacketType = 14 // In
	TypeConfigure         PacketType = 15 // In
)

// Mode is the single-character FT mode marker used on Decode/Reply messages.
type Mode string

const (
	ModeFT8 Mode = "~"
	ModeFT4 Mode = "+"
)

// Name returns the human-readable name ("FT8"/"FT4") for the marker, or the raw
// value if it is not a known marker.
func (m Mode) Name() string {
	switch m {
	case ModeFT8:
		return "FT8"
	case ModeFT4:
		return "FT4"
	default:
		return string(m)
	}
}

// ModeFromName maps "FT8"/"FT4" to the single-character marker. Unknown names
// are returned unchanged.
func ModeFromName(name string) Mode {
	switch name {
	case "FT8":
		return ModeFT8
	case "FT4":
		return ModeFT4
	default:
		return Mode(name)
	}
}

// Modifier is a Qt keyboard-modifier bitmask carried on Reply messages.
type Modifier uint8

const (
	NoModifier  Modifier = 0x00
	ShiftMod    Modifier = 0x02
	CtrlMod     Modifier = 0x04
	AltMod      Modifier = 0x08
	MetaMod     Modifier = 0x10
	KeypadMod   Modifier = 0x20
	GroupSwitch Modifier = 0x40
)

// SOMode is the special-operating mode reported in Status messages.
type SOMode uint8

const (
	SONone     SOMode = 0
	SONAVHF    SOMode = 1
	SOEUVHF    SOMode = 2
	SOFieldDay SOMode = 3
	SORTTYRU   SOMode = 4
	SOWWDigi   SOMode = 5
	SOFox      SOMode = 6
	SOHound    SOMode = 7
	SOARRLDigi SOMode = 8
)
