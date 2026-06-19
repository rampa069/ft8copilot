package adif

import (
	"strings"
	"testing"
)

// sampleQRZ mimics the real QRZ Logbook export: an indented free-text header
// with a few tags before <eoh>, then lowercase-tagged records — including a
// length-prefixed value containing spaces, and a second record with no grid.
const sampleQRZ = `QRZLogbook download for ea5iue
    Records: 2
    <ADIF_VER:5>3.1.1
    <PROGRAMID:10>QRZLogbook
    <eoh>
<call:6>EA5EKP
<qsl_via:47>EQSL.CC   BUREAU   DIREC - BOX - 5031 Cp- 30205
<band:2>2m
<freq:5>145.7
<gridsquare:6>IM97mo
<mode:2>FM
<qso_date:8>20180405
<time_on:4>1030
<country:5>Spain
<eor>

<call:5>W6BSD
<band:3>20m
<freq:6>14.074
<mode:3>FT8
<qso_date:8>20210101
<eor>
`

func TestParseQRZSample(t *testing.T) {
	recs, err := Parse(strings.NewReader(sampleQRZ))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}

	r0 := recs[0]
	if v, _ := r0.Get("call"); v != "EA5EKP" {
		t.Errorf("call = %q, want EA5EKP", v)
	}
	if v, _ := r0.Get("QSL_VIA"); v != "EQSL.CC   BUREAU   DIREC - BOX - 5031 Cp- 30205" {
		t.Errorf("qsl_via with spaces not read by length: %q", v)
	}
	if v, _ := r0.Get("band"); v != "2m" {
		t.Errorf("band = %q, want 2m", v)
	}
	if v, _ := r0.Get("gridsquare"); v != "IM97mo" {
		t.Errorf("gridsquare = %q, want IM97mo", v)
	}

	// Second record has no grid — must still parse cleanly.
	r1 := recs[1]
	if v, _ := r1.Get("call"); v != "W6BSD" {
		t.Errorf("call = %q, want W6BSD", v)
	}
	if _, ok := r1.Get("gridsquare"); ok {
		t.Errorf("second record should have no gridsquare")
	}
	if v, _ := r1.Get("band"); v != "20m" {
		t.Errorf("band = %q, want 20m", v)
	}
}

func TestParseHeaderless(t *testing.T) {
	// No header: the file starts directly with a tag.
	in := "<call:5>K1ABC<band:3>40m<eor>"
	recs, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if v, _ := recs[0].Get("CALL"); v != "K1ABC" {
		t.Errorf("call = %q, want K1ABC", v)
	}
}

func TestParseValueContainingAngleBrackets(t *testing.T) {
	// A value containing < and > must be read by length, not by delimiter.
	in := "<comment:4><a>b<eor>"
	recs, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v, _ := recs[0].Get("comment"); v != "<a>b" {
		t.Errorf("comment = %q, want <a>b", v)
	}
}

func TestParseFinalRecordWithoutEOR(t *testing.T) {
	in := "<call:5>K1ABC<band:3>40m"
	recs, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1 (tolerate missing <eor>)", len(recs))
	}
	if v, _ := recs[0].Get("band"); v != "40m" {
		t.Errorf("band = %q, want 40m", v)
	}
}

func TestParseEmptyValue(t *testing.T) {
	in := "<call:5>K1ABC<name:0><band:3>40m<eor>"
	recs, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v, ok := recs[0].Get("name"); !ok || v != "" {
		t.Errorf("empty value: got %q ok=%v, want \"\" true", v, ok)
	}
	if v, _ := recs[0].Get("band"); v != "40m" {
		t.Errorf("band after empty value = %q, want 40m", v)
	}
}

func TestParseEmptyInput(t *testing.T) {
	recs, err := Parse(strings.NewReader("   \n\t  "))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("empty input got %d records, want 0", len(recs))
	}
}

func TestReaderStreaming(t *testing.T) {
	r := NewReader(strings.NewReader(sampleQRZ))
	n := 0
	for {
		_, err := r.Next()
		if err != nil {
			break
		}
		n++
	}
	if n != 2 {
		t.Errorf("streamed %d records, want 2", n)
	}
}
