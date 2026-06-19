package adif

import (
	"strings"
	"testing"
)

func TestEncodeRecord(t *testing.T) {
	var b strings.Builder
	e := NewEncoder(&b)
	if err := e.WriteRecord(Record{"CALL": "CO8LY", "BAND": "20m", "EMPTY": ""}); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	if err := e.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	out := b.String()
	// Sorted, non-empty fields with byte lengths, then <EOR>.
	want := "<BAND:3>20m\n<CALL:5>CO8LY\n<EOR>\n"
	if out != want {
		t.Errorf("encode =\n%q\nwant\n%q", out, want)
	}
}

func TestEncodeHeader(t *testing.T) {
	var b strings.Builder
	e := NewEncoder(&b)
	_ = e.WriteHeader("FT8CoPilot export", Record{"ADIF_VER": "3.1.1", "PROGRAMID": "FT8CoPilot"})
	_ = e.Flush()
	out := b.String()
	for _, want := range []string{"FT8CoPilot export\n", "<ADIF_VER:5>3.1.1\n", "<PROGRAMID:10>FT8CoPilot\n", "<EOH>\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q in:\n%s", want, out)
		}
	}
}

func TestEncodeUTF8ByteLength(t *testing.T) {
	var b strings.Builder
	e := NewEncoder(&b)
	_ = e.WriteRecord(Record{"COUNTRY": "España"}) // 'ñ' is 2 bytes
	_ = e.Flush()
	if !strings.Contains(b.String(), "<COUNTRY:7>España") {
		t.Errorf("byte length wrong:\n%s", b.String())
	}
}

func TestEncodeParseRoundTrip(t *testing.T) {
	records := []Record{
		{"CALL": "CO8LY", "BAND": "20m", "GRIDSQUARE": "FL11"},
		{"CALL": "W6BSD", "BAND": "40m"},
	}
	var b strings.Builder
	e := NewEncoder(&b)
	_ = e.WriteHeader("test", Record{"ADIF_VER": "3.1.1"})
	for _, r := range records {
		_ = e.WriteRecord(r)
	}
	_ = e.Flush()

	got, err := Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("round-trip got %d records, want %d", len(got), len(records))
	}
	for i, want := range records {
		for k, v := range want {
			if gv, _ := got[i].Get(k); gv != v {
				t.Errorf("record %d field %s = %q, want %q", i, k, gv, v)
			}
		}
	}
}
