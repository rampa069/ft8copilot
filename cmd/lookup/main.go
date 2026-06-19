// Command lookup inspects the cqcalls database: a refreshing table view and
// filters by call/country/status/band, plus record deletion.
//
// Port of lookup.py. See FT8CoPilot-rxn.13.
//
// Actions (exactly one required):
//
//	-d/--delete CALL   delete a record by call+band (requires -b/--band)
//	-r/--run           continuously print the rows seen in the last interval
//	                   seconds (default 30), refreshing every 15s; the optional
//	                   interval is supplied with --interval
//	-c/--call CALL     find rows whose call matches the regexp CALL
//	--country COUNTRY  find rows for a country
//	--status STATUS    find rows with a status
//
// The optional -b/--band int filters --call/--country/--status by band.
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rampa069/ft8copilot/internal/config"
	"github.com/rampa069/ft8copilot/internal/db"
	"github.com/rampa069/ft8copilot/internal/lotw"
)

// runTime is the default window (seconds) for the --run view, matching
// RUN_TIME in lookup.py.
const runTime = 30

// refreshInterval is how long --run sleeps between redraws.
const refreshInterval = 15 * time.Second

// lotwLookup reports whether a callsign is a known LOTW user. A nil receiver
// (LOTW unavailable) always reports false.
type lotwLookup struct {
	cache *lotw.Cache
}

func (l lotwLookup) contains(call string) bool {
	if l.cache == nil {
		return false
	}
	return l.cache.Contains(call)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lookup:", err)
		os.Exit(1)
	}
}

// run parses args and dispatches to the requested action.
func run(args []string) error {
	fs := flag.NewFlagSet("lookup", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: lookup [-C config] <action> [-b band]\n\n")
		_, _ = fmt.Fprintf(fs.Output(), "Exactly one action is required:\n")
		_, _ = fmt.Fprintf(fs.Output(), "  -d, --delete CALL    delete a record (requires -b/--band)\n")
		_, _ = fmt.Fprintf(fs.Output(), "  -r, --run            refreshing view of recent rows (see --interval)\n")
		_, _ = fmt.Fprintf(fs.Output(), "  -c, --call CALL      find rows whose call matches the regexp CALL\n")
		_, _ = fmt.Fprintf(fs.Output(), "      --country NAME   find rows for a country\n")
		_, _ = fmt.Fprintf(fs.Output(), "      --status N       find rows with status N\n\n")
		_, _ = fmt.Fprintf(fs.Output(), "Options:\n")
		fs.PrintDefaults()
	}

	var (
		configPath string
		deleteCall string
		runView    bool
		interval   int
		call       string
		country    string
		status     int
		statusSet  bool
		band       int
		bandSet    bool
	)

	fs.StringVar(&configPath, "C", "", "configuration file (default: search standard locations)")
	fs.StringVar(&configPath, "config", "", "configuration file (default: search standard locations)")
	fs.StringVar(&deleteCall, "d", "", "delete entry for CALL (requires -b/--band)")
	fs.StringVar(&deleteCall, "delete", "", "delete entry for CALL (requires -b/--band)")
	fs.BoolVar(&runView, "r", false, "run continuously, refreshing the recent-rows view")
	fs.BoolVar(&runView, "run", false, "run continuously, refreshing the recent-rows view")
	fs.IntVar(&interval, "interval", runTime, "window in seconds for --run")
	fs.StringVar(&call, "c", "", "callsign regexp")
	fs.StringVar(&call, "call", "", "callsign regexp")
	fs.StringVar(&country, "country", "", "country name")
	fs.IntVar(&status, "status", 0, "status value")
	fs.IntVar(&band, "b", 0, "band (meters)")
	fs.IntVar(&band, "band", 0, "band (meters)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Track which flags were explicitly provided (for the int actions/filters,
	// whose zero value is a legitimate input).
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "status":
			statusSet = true
		case "b", "band":
			bandSet = true
		}
	})

	deleteCall = strings.ToUpper(deleteCall)
	call = strings.ToUpper(call)

	// Enforce exactly one action.
	var actions []string
	if deleteCall != "" {
		actions = append(actions, "delete")
	}
	if runView {
		actions = append(actions, "run")
	}
	if call != "" {
		actions = append(actions, "call")
	}
	if country != "" {
		actions = append(actions, "country")
	}
	if statusSet {
		actions = append(actions, "status")
	}
	if len(actions) != 1 {
		fs.Usage()
		return fmt.Errorf("exactly one action (--delete, --run, --call, --country, --status) is required")
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	store, err := db.Open(cfg.FT8Ctrl.DBName)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	var bandPtr *int
	if bandSet {
		bandPtr = &band
	}

	switch actions[0] {
	case "delete":
		if !bandSet {
			fmt.Println("Argument --band is missing")
			return nil
		}
		return deleteRecord(store, deleteCall, band)
	case "run":
		lk := loadLOTW()
		return runViewLoop(store, lk, time.Duration(interval)*time.Second)
	case "call":
		return findByCall(store, call, bandPtr)
	case "country":
		recs, err := store.FindByCountry(country, bandPtr)
		if err != nil {
			return err
		}
		return printRecords(os.Stdout, recs, loadLOTW())
	case "status":
		recs, err := store.FindByStatus(status, bandPtr)
		if err != nil {
			return err
		}
		return printRecords(os.Stdout, recs, loadLOTW())
	}
	return nil
}

// loadConfig loads the configuration from path, or searches the standard
// locations when path is empty.
func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		return config.Load(path)
	}
	return config.Find()
}

// loadLOTW opens the LOTW cache, falling back to a disabled lookup (with a
// stderr warning) if it is unavailable. This keeps the CLI usable offline.
func loadLOTW() lotwLookup {
	cache, err := lotw.Default()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lookup: warning: LOTW lookup disabled:", err)
		return lotwLookup{}
	}
	return lotwLookup{cache: cache}
}

// deleteRecord removes a call+band record and reports the outcome.
func deleteRecord(store *db.Store, call string, band int) error {
	n, err := store.DeleteCallBand(call, band)
	if err != nil {
		return err
	}
	action := "Not found"
	if n > 0 {
		action = "Deleted"
	}
	fmt.Printf("%s on %dm band - %s\n", call, band, action)
	return nil
}

// findByCall fetches rows (optionally band-filtered) and prints those whose
// call matches the given regexp, mirroring Python's re.search semantics.
func findByCall(store *db.Store, expr string, band *int) error {
	re, err := regexp.Compile(expr)
	if err != nil {
		return fmt.Errorf("invalid call regexp %q: %w", expr, err)
	}
	all, err := store.All(band)
	if err != nil {
		return err
	}
	var matched []db.Record
	for _, rec := range all {
		if re.MatchString(rec.Call) {
			matched = append(matched, rec)
		}
	}
	return printRecords(os.Stdout, matched, loadLOTW())
}

// runViewLoop clears the screen and prints the rows seen within the last delta,
// refreshing every refreshInterval, until interrupted.
func runViewLoop(store *db.Store, lk lotwLookup, delta time.Duration) error {
	clearScreen()
	for {
		recs, err := store.Since(delta)
		if err != nil {
			return err
		}
		if len(recs) > 0 {
			clearScreen()
			if err := printRecords(os.Stdout, recs, lk); err != nil {
				return err
			}
			fmt.Println()
		}
		time.Sleep(refreshInterval)
	}
}

// clearScreen clears the terminal on posix systems.
func clearScreen() {
	if isPosix() {
		fmt.Print("\033[H\033[2J")
	}
}

// isPosix reports whether the OS is posix-like (everything but Windows here).
func isPosix() bool {
	return os.PathSeparator == '/'
}

// printRecords renders records as a tab-aligned table with a header row,
// annotating each with LOTW membership. Columns match KEYS in lookup.py plus
// the computed lotw column.
func printRecords(out *os.File, recs []db.Record, lk lotwLookup) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if err := renderTable(w, recs, lk); err != nil {
		return err
	}
	return w.Flush()
}

// renderTable writes the table to w. It is separated from printRecords so it
// can be unit-tested against any io.Writer.
func renderTable(w interface{ Write([]byte) (int, error) }, recs []db.Record, lk lotwLookup) error {
	header := "call\tstatus\tband\tsnr\tgrid\tcqzone\tituzone\tcountry\tcontinent\ttime\textra\tlotw"
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}
	for _, r := range recs {
		l := "-"
		if lk.contains(r.Call) {
			l = "true"
		}
		_, err := fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
			r.Call, r.Status, r.Band, r.SNR, r.Grid, r.CQZone, r.ITUZone,
			r.Country, r.Continent, r.Time.Format("2006-01-02 15:04:05"), r.Extra, l)
		if err != nil {
			return err
		}
	}
	return nil
}
