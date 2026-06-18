package db

import "time"

// Packet is the decoded FT8/FT4 spot carried with an Insert and stored as JSON
// in the cqcalls.packet column. It mirrors the dict produced by WSDecode in the
// original (wsjtx.py). Unlike the Python implementation — which used a bespoke
// JSON codec for datetime/set values — this stores Time as a standard JSON
// time.Time; the on-disk JSON is therefore not wire-compatible with the Python
// database, which is fine because the database is recreated by this port.
type Packet struct {
	New            bool      `json:"New"`
	Time           time.Time `json:"Time"`
	SNR            int32     `json:"SNR"`
	DeltaTime      float64   `json:"DeltaTime"`
	DeltaFrequency uint32    `json:"DeltaFrequency"`
	Mode           string    `json:"Mode"` // raw marker, "~" (FT8) or "+" (FT4)
	Message        string    `json:"Message"`
	LowConfidence  bool      `json:"LowConfidence"`
	OffAir         bool      `json:"OffAir"`
}
