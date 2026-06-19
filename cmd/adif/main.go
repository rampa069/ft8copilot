// Command adif imports and exports the cqcalls database in ADIF format (the
// de-facto log-exchange format of WSJT-X, QRZ, LoTW, …).
//
//	adif -c ft8ctrl.yaml import mylog.adi             # import, marking QSOs worked
//	adif -c ft8ctrl.yaml import --dry-run mylog.adi   # report only, no writes
//	adif -c ft8ctrl.yaml export worked.adi            # export worked rows
//	adif -c ft8ctrl.yaml export --all --band 20 -     # filtered export to stdout
//
// Importing seeds the "worked" state the DXCC100 selector and already-worked
// filtering rely on, so the automation behaves correctly from the first run.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rampa069/ft8copilot/internal/adif"
	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/dxcc"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "adif:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: adif [-c config] <command> [options] <file>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  import <file.adi> [--dry-run]              import a log, marking QSOs worked\n")
	fmt.Fprintf(os.Stderr, "  export <file.adi|-> [--all] [--band N] [--status N]  export rows to ADIF\n")
}

// run dispatches to a subcommand. A leading -c/--config applies to whichever
// subcommand follows.
func run(args []string) error {
	gf := flag.NewFlagSet("adif", flag.ContinueOnError)
	var configPath string
	gf.StringVar(&configPath, "c", "", "configuration file (default: search standard locations)")
	gf.StringVar(&configPath, "config", "", "configuration file (default: search standard locations)")
	gf.Usage = usage
	if err := gf.Parse(args); err != nil {
		return err
	}
	rest := gf.Args()
	if len(rest) == 0 {
		usage()
		return fmt.Errorf("a subcommand is required: import or export")
	}
	switch rest[0] {
	case "import":
		return runImport(configPath, rest[1:])
	case "export":
		return runExport(configPath, rest[1:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", rest[0])
	}
}

func runImport(configPath string, args []string) error {
	fs := flag.NewFlagSet("adif import", flag.ContinueOnError)
	cfgPath := configPath
	fs.StringVar(&cfgPath, "c", cfgPath, "configuration file")
	fs.StringVar(&cfgPath, "config", cfgPath, "configuration file")
	dryRun := fs.Bool("dry-run", false, "parse and report without writing to the database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: adif import <file.adi> [--dry-run]")
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	stats := newStats()
	if *dryRun {
		return dryRunImport(f, stats)
	}

	store, err := db.Open(cfg.FT8Ctrl.DBName)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	entities, err := dxcc.New()
	if err != nil {
		return fmt.Errorf("dxcc: %w", err)
	}
	writer, err := db.NewWriter(store, cfg.FT8Ctrl.MyGrid, entities, nil)
	if err != nil {
		return fmt.Errorf("db writer: %w", err)
	}

	reader := adif.NewReader(f)
	for {
		rec, err := reader.Next()
		if err != nil {
			break // io.EOF or malformed tail; stop at what we have
		}
		stats.total++
		qso, reason := recordToQSO(rec)
		if reason != "" {
			stats.skip(reason)
			continue
		}
		ok, err := writer.ImportWorked(qso)
		if err != nil {
			return fmt.Errorf("import %s: %w", qso.Call, err)
		}
		if !ok {
			stats.skip("no country")
			continue
		}
		stats.imported(qso.Band)
	}

	stats.print(false)
	return nil
}

// dryRunImport parses and classifies records without touching the database or
// resolving DXCC (so it cannot report per-call country skips).
func dryRunImport(f *os.File, stats *importStats) error {
	reader := adif.NewReader(f)
	for {
		rec, err := reader.Next()
		if err != nil {
			break
		}
		stats.total++
		qso, reason := recordToQSO(rec)
		if reason != "" {
			stats.skip(reason)
			continue
		}
		stats.imported(qso.Band)
	}
	stats.print(true)
	return nil
}

// recordToQSO maps an ADIF record to a WorkedQSO, returning a non-empty reason
// when the record cannot be imported.
func recordToQSO(rec adif.Record) (db.WorkedQSO, string) {
	call := strings.ToUpper(strings.TrimSpace(get(rec, "CALL")))
	if call == "" {
		return db.WorkedQSO{}, "no call"
	}

	freqHz := parseFreqHz(get(rec, "FREQ"))
	band := db.BandMetersFromName(get(rec, "BAND"))
	if band == 0 && freqHz > 0 {
		band = db.Band(freqHz)
	}
	if band == 0 {
		return db.WorkedQSO{}, "no band"
	}

	cqz, _ := strconv.Atoi(strings.TrimSpace(get(rec, "CQZ")))
	ituz, _ := strconv.Atoi(strings.TrimSpace(get(rec, "ITUZ")))

	return db.WorkedQSO{
		Call:      call,
		Band:      band,
		Grid:      strings.TrimSpace(get(rec, "GRIDSQUARE")),
		Frequency: freqHz,
		Time:      parseQSOTime(get(rec, "QSO_DATE"), get(rec, "TIME_ON")),
		Country:   strings.TrimSpace(get(rec, "COUNTRY")),
		Continent: strings.ToUpper(strings.TrimSpace(get(rec, "CONT"))),
		CQZone:    cqz,
		ITUZone:   ituz,
	}, ""
}

func get(rec adif.Record, name string) string {
	v, _ := rec.Get(name)
	return v
}

// parseFreqHz converts an ADIF FREQ field (MHz, e.g. "14.074") to Hz.
func parseFreqHz(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mhz, err := strconv.ParseFloat(s, 64)
	if err != nil || mhz <= 0 {
		return 0
	}
	return uint64(mhz * 1e6)
}

// parseQSOTime combines an ADIF QSO_DATE (YYYYMMDD) and TIME_ON (HHMM or
// HHMMSS) into a UTC time. A missing/invalid date yields the zero time (the
// stamp is unused for worked rows).
func parseQSOTime(date, tm string) time.Time {
	date = strings.TrimSpace(date)
	tm = strings.TrimSpace(tm)
	if len(date) != 8 {
		return time.Time{}
	}
	layout, val := "20060102", date
	switch {
	case len(tm) >= 6:
		layout, val = "20060102150405", date+tm[:6]
	case len(tm) >= 4:
		layout, val = "200601021504", date+tm[:4]
	}
	t, err := time.ParseInLocation(layout, val, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// importStats accumulates a run's counts.
type importStats struct {
	total   int
	imp     int
	skipped int
	reasons map[string]int
	perBand map[int]int
}

func newStats() *importStats {
	return &importStats{reasons: map[string]int{}, perBand: map[int]int{}}
}

func (s *importStats) skip(reason string) {
	s.skipped++
	s.reasons[reason]++
}

func (s *importStats) imported(band int) {
	s.imp++
	s.perBand[band]++
}

func (s *importStats) print(dry bool) {
	fmt.Printf("parsed %d records\n", s.total)
	if dry {
		fmt.Printf("would import %d QSOs (dry run — DXCC not resolved)\n", s.imp)
	} else {
		fmt.Printf("imported %d QSOs (marked worked)\n", s.imp)
	}
	if s.skipped > 0 {
		fmt.Printf("skipped %d (%s)\n", s.skipped, formatReasons(s.reasons))
	}
	if len(s.perBand) > 0 {
		fmt.Printf("by band: %s\n", formatBands(s.perBand))
	}
}

func formatReasons(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func formatBands(m map[int]int) string {
	bands := make([]int, 0, len(m))
	for b := range m {
		bands = append(bands, b)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(bands)))
	parts := make([]string, 0, len(bands))
	for _, b := range bands {
		parts = append(parts, fmt.Sprintf("%dm=%d", b, m[b]))
	}
	return strings.Join(parts, "  ")
}

// loadConfig mirrors the other commands: an explicit path or a standard-location
// search.
func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		return config.Load(path)
	}
	return config.Find()
}
