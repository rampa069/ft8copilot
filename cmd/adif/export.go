package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/rampa069/ft8copilot/internal/adif"
	"github.com/rampa069/ft8copilot/internal/db"
)

func runExport(configPath string, args []string) error {
	fs := flag.NewFlagSet("adif export", flag.ContinueOnError)
	cfgPath := configPath
	fs.StringVar(&cfgPath, "c", cfgPath, "configuration file")
	fs.StringVar(&cfgPath, "config", cfgPath, "configuration file")
	all := fs.Bool("all", false, "export every row (default: only worked rows)")
	band := fs.Int("band", 0, "filter by band in metres")
	status := fs.Int("status", -1, "filter by status (0 new, 1 in progress, 2 worked)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: adif export <file.adi|-> [--all] [--band N] [--status N]")
	}
	outPath := fs.Arg(0)

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	store, err := db.Open(cfg.FT8Ctrl.DBName)
	if err != nil {
		return err
	}
	defer store.Close()

	q := db.Query{}
	if *band != 0 {
		q.Band = band
	}
	switch {
	case *status >= 0:
		q.Status = status
	case !*all:
		worked := 2
		q.Status = &worked // default: only worked rows
	}

	rows, err := store.Search(q)
	if err != nil {
		return err
	}

	out := os.Stdout
	if outPath != "-" {
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	if err := writeADIF(out, rows, cfg.FT8Ctrl.MyCall, cfg.FT8Ctrl.MyGrid); err != nil {
		return err
	}

	// Summary to stderr so stdout stays clean ADIF when exporting to '-'.
	perBand := map[int]int{}
	for _, r := range rows {
		perBand[r.Band]++
	}
	fmt.Fprintf(os.Stderr, "exported %d records\n", len(rows))
	if len(perBand) > 0 {
		fmt.Fprintf(os.Stderr, "by band: %s\n", formatBands(perBand))
	}
	return nil
}

// writeADIF encodes the rows as an ADIF document with a small header.
func writeADIF(w io.Writer, rows []db.Record, myCall, myGrid string) error {
	enc := adif.NewEncoder(w)
	if err := enc.WriteHeader("FT8CoPilot export", adif.Record{
		"ADIF_VER":  "3.1.1",
		"PROGRAMID": "FT8CoPilot",
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := enc.WriteRecord(recordToADIF(r, myCall, myGrid)); err != nil {
			return err
		}
	}
	return enc.Flush()
}

// recordToADIF maps a cqcalls row to an ADIF record. Mode defaults to FT8 (the
// daemon's mode; the cryptic stored packet mode isn't a valid ADIF MODE).
func recordToADIF(r db.Record, myCall, myGrid string) adif.Record {
	rec := adif.Record{
		"CALL": r.Call,
		"BAND": fmt.Sprintf("%dm", r.Band),
		"MODE": "FT8",
	}
	if !r.Time.IsZero() {
		rec["QSO_DATE"] = r.Time.UTC().Format("20060102")
		rec["TIME_ON"] = r.Time.UTC().Format("150405")
	}
	if r.Frequency > 0 {
		rec["FREQ"] = strconv.FormatFloat(float64(r.Frequency)/1e6, 'f', 6, 64)
	}
	if r.Grid != "" {
		rec["GRIDSQUARE"] = r.Grid
	}
	if r.Country != "" {
		rec["COUNTRY"] = r.Country
	}
	if r.Continent != "" {
		rec["CONT"] = r.Continent
	}
	if r.CQZone != 0 {
		rec["CQZ"] = strconv.Itoa(r.CQZone)
	}
	if r.ITUZone != 0 {
		rec["ITUZ"] = strconv.Itoa(r.ITUZone)
	}
	if myCall != "" {
		rec["STATION_CALLSIGN"] = myCall
	}
	if myGrid != "" {
		rec["MY_GRIDSQUARE"] = myGrid
	}
	return rec
}
