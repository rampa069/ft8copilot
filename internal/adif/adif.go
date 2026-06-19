// Package adif is a small, tolerant streaming parser for ADIF (Amateur Data
// Interchange Format) log files, the de-facto export format of WSJT-X, QRZ,
// LoTW and most logging programs.
//
// ADIF structure: an optional free-text header terminated by <EOH>, followed by
// records. Each record is a run of fields and ends with <EOR>. A field is
// "<name:length[:type]>value", where value is exactly length bytes and may
// contain any character (including < and >), so the parser reads values by
// length rather than by delimiter. Tag names are case-insensitive.
//
// The parser is deliberately lenient: it tolerates a missing header (a file
// whose first non-blank character is '<' has no header), CRLF line endings,
// arbitrary whitespace between fields, unknown/extra fields, and a final record
// without a trailing <EOR>.
package adif

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Record is one ADIF record: field name (upper-cased) to value. Values are kept
// verbatim (not trimmed) since some fields are fixed-width.
type Record map[string]string

// Get returns the value for a field name (case-insensitive) and whether it was
// present.
func (r Record) Get(name string) (string, bool) {
	v, ok := r[strings.ToUpper(name)]
	return v, ok
}

// ctrlKind classifies a parsed tag.
type ctrlKind int

const (
	ctrlNone ctrlKind = iota // a normal data field
	ctrlEOR                  // <eor>: end of record
	ctrlEOH                  // <eoh>: end of header
)

// Reader streams records from an ADIF source.
type Reader struct {
	r       *bufio.Reader
	started bool // header consumed
}

// NewReader builds a Reader over rd.
func NewReader(rd io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(rd)}
}

// Parse reads every record from rd.
func Parse(rd io.Reader) ([]Record, error) {
	r := NewReader(rd)
	var out []Record
	for {
		rec, err := r.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, rec)
	}
}

// Next returns the next record, or io.EOF when the input is exhausted. The first
// call consumes the header (if any).
func (rd *Reader) Next() (Record, error) {
	if !rd.started {
		if err := rd.skipHeader(); err != nil {
			return nil, err // includes io.EOF for an empty file
		}
		rd.started = true
	}

	rec := Record{}
	got := false
	for {
		name, val, ctrl, err := rd.readField()
		if err != nil {
			if err == io.EOF && got {
				return rec, nil // tolerate a final record with no <eor>
			}
			return nil, err
		}
		switch ctrl {
		case ctrlEOR:
			return rec, nil
		case ctrlEOH:
			continue // stray header terminator; ignore
		default:
			if name != "" {
				rec[strings.ToUpper(name)] = val
				got = true
			}
		}
	}
}

// skipHeader detects and discards the header. A file whose first non-blank byte
// is '<' has no header; otherwise everything up to and including <eoh> is
// discarded (header fields are parsed by length so their values can't confuse
// the scan).
func (rd *Reader) skipHeader() error {
	for {
		b, err := rd.r.ReadByte()
		if err != nil {
			return err
		}
		if isSpace(b) {
			continue
		}
		if b == '<' {
			_ = rd.r.UnreadByte() // headerless: leave the tag for readField
			return nil
		}
		// Header text: discard fields until <eoh>.
		for {
			_, _, ctrl, err := rd.readField()
			if err != nil {
				return err
			}
			if ctrl == ctrlEOH {
				return nil
			}
		}
	}
}

// readField scans to the next tag and parses it. For a data field it returns the
// name and value; for <eor>/<eoh> it returns the corresponding ctrlKind.
func (rd *Reader) readField() (name, val string, ctrl ctrlKind, err error) {
	// Advance to the start of a tag.
	for {
		b, e := rd.r.ReadByte()
		if e != nil {
			return "", "", ctrlNone, e
		}
		if b == '<' {
			break
		}
	}

	spec, e := rd.r.ReadString('>')
	if e != nil {
		return "", "", ctrlNone, e
	}
	spec = strings.TrimSuffix(spec, ">")

	parts := strings.Split(spec, ":")
	name = strings.TrimSpace(parts[0])
	switch strings.ToLower(name) {
	case "eor":
		return "", "", ctrlEOR, nil
	case "eoh":
		return "", "", ctrlEOH, nil
	}

	if len(parts) < 2 {
		// Malformed data field (no length): treat as empty, keep going.
		return name, "", ctrlNone, nil
	}
	length, e := strconv.Atoi(strings.TrimSpace(parts[1]))
	if e != nil || length < 0 {
		return name, "", ctrlNone, nil // tolerate a bad length
	}

	buf := make([]byte, length)
	if _, e := io.ReadFull(rd.r, buf); e != nil {
		return "", "", ctrlNone, e
	}
	return name, string(buf), ctrlNone, nil
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
