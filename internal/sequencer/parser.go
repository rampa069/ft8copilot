package sequencer

import "regexp"

// msgKind classifies a decoded FT8/FT4 message.
type msgKind int

const (
	msgNone  msgKind = iota
	msgCQ            // a station calling CQ
	msgReply         // a station replying to someone
)

// parsed holds the fields extracted from a decoded message.
type parsed struct {
	kind  msgKind
	to    string // REPLY: the station being answered
	call  string // the transmitting station
	grid  string // CQ: 4-char Maidenhead grid (may be empty for a broken CQ)
	extra string // CQ: optional tag between "CQ" and the call (e.g. "DX", "POTA")
}

// Message-parsing regexps. Go's regexp (RE2) has no lookahead, so the REPLY
// pattern from the original — which used (?!CQ) to avoid matching CQ lines — is
// emulated by matching first and rejecting a "to" field of "CQ" in code.
var (
	replyRe    = regexp.MustCompile(`^(?P<to>\w+)(?:/\w+)? (?P<call>\w+)(?:/\w+)? .+`)
	cqRe       = regexp.MustCompile(`^CQ\s(?:CQ\s|(?P<extra>\S+)\s|)(?P<call>\w+(?:/\w+)?)\s(?P<grid>[A-Z]{2}[0-9]{2})`)
	brokenCQRe = regexp.MustCompile(`^CQ\s(?P<call>\w+(?:/\w+)?)$`)
)

// parseMessage classifies a decoded message and extracts its fields, mirroring
// the PARSERS table and parser() in ft8ctrl.py (order: REPLY, CQ, BROKENCQ).
func parseMessage(message string) parsed {
	// REPLY: "<to> <call> <report...>", but a leading "CQ" is not a reply.
	if m := replyRe.FindStringSubmatch(message); m != nil {
		to := m[replyRe.SubexpIndex("to")]
		if to != "CQ" {
			return parsed{
				kind: msgReply,
				to:   to,
				call: m[replyRe.SubexpIndex("call")],
			}
		}
	}
	// CQ with grid: "CQ [tag] <call> <grid>".
	if m := cqRe.FindStringSubmatch(message); m != nil {
		return parsed{
			kind:  msgCQ,
			call:  m[cqRe.SubexpIndex("call")],
			grid:  m[cqRe.SubexpIndex("grid")],
			extra: m[cqRe.SubexpIndex("extra")],
		}
	}
	// Broken CQ: "CQ <call>" with no grid.
	if m := brokenCQRe.FindStringSubmatch(message); m != nil {
		return parsed{kind: msgCQ, call: m[brokenCQRe.SubexpIndex("call")]}
	}
	return parsed{kind: msgNone}
}
