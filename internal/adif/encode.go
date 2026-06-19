package adif

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Encoder writes ADIF text to an io.Writer: an optional header followed by
// records. It is the inverse of Reader; Parse(encode(records)) round-trips.
type Encoder struct {
	w *bufio.Writer
}

// NewEncoder builds an Encoder over w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriter(w)}
}

// WriteHeader writes a free-text comment line (if non-empty) and the given
// header fields, terminated by <EOH>. Fields are written in sorted order for
// determinism. Call it at most once, before any records; for a headerless file
// skip it entirely.
func (e *Encoder) WriteHeader(comment string, fields Record) error {
	if comment != "" {
		if _, err := fmt.Fprintln(e.w, comment); err != nil {
			return err
		}
	}
	if err := e.writeFields(fields); err != nil {
		return err
	}
	_, err := io.WriteString(e.w, "<EOH>\n")
	return err
}

// WriteRecord writes one record's fields (non-empty, sorted) followed by <EOR>.
func (e *Encoder) WriteRecord(rec Record) error {
	if err := e.writeFields(rec); err != nil {
		return err
	}
	_, err := io.WriteString(e.w, "<EOR>\n")
	return err
}

// Flush flushes buffered output; call it once after the last record.
func (e *Encoder) Flush() error { return e.w.Flush() }

// writeFields emits each non-empty field as "<NAME:bytelen>value", sorted by
// name. The length is the byte count (ADIF measures values in bytes, so UTF-8
// values are counted correctly).
func (e *Encoder) writeFields(rec Record) error {
	names := make([]string, 0, len(rec))
	for name, val := range rec {
		if val != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		val := rec[name]
		if _, err := fmt.Fprintf(e.w, "<%s:%d>%s\n", strings.ToUpper(name), len(val), val); err != nil {
			return err
		}
	}
	return nil
}
