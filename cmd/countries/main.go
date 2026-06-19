// Command countries is a DXCC lookup helper: list entities, resolve a
// callsign/prefix, check an entity exists and list an entity's prefixes.
//
// Port of countries.py. See FT8CoPilot-rxn.14.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rampa069/ft8copilot/internal/dxcc"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("countries", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Helper program for looking up DXCC entities.")
		fmt.Fprintln(os.Stderr, "Usage: countries (-l | -c CALL | -C NAME | -p NAME)")
		fs.PrintDefaults()
	}

	var (
		list    bool
		country string
		check   string
		prefix  string
	)
	fs.BoolVar(&list, "l", false, "List all the countries")
	fs.BoolVar(&list, "list", false, "List all the countries")
	fs.StringVar(&country, "c", "", "Find the country from a callsign or a prefix")
	fs.StringVar(&country, "country", "", "Find the country from a callsign or a prefix")
	fs.StringVar(&check, "C", "", "Check if a country exists")
	fs.StringVar(&check, "check", "", "Check if a country exists")
	fs.StringVar(&prefix, "p", "", "List all the prefixes for a given country")
	fs.StringVar(&prefix, "prefix", "", "List all the prefixes for a given country")

	if err := fs.Parse(args); err != nil {
		return errors.New("Argument Error")
	}

	// Enforce the required, mutually-exclusive group: exactly one flag.
	chosen := 0
	if list {
		chosen++
	}
	if country != "" {
		chosen++
	}
	if check != "" {
		chosen++
	}
	if prefix != "" {
		chosen++
	}
	if chosen != 1 {
		fs.Usage()
		return errors.New("exactly one of -l, -c, -C or -p is required")
	}

	d, err := dxcc.New()
	if err != nil {
		return err
	}

	switch {
	case list:
		return doList(d)
	case country != "":
		return doCountry(d, country)
	case check != "":
		return doCheck(d, check)
	default:
		return doPrefix(d, prefix)
	}
}

// doList prints every DXCC entity, one per line (Entities is already sorted).
func doList(d *dxcc.DXCC) error {
	for _, name := range d.Entities() {
		fmt.Println(name)
	}
	return nil
}

// doCountry resolves a callsign/prefix and prints the matched entity, mirroring
// countries.py get_prefix.
func doCountry(d *dxcc.DXCC, input string) error {
	input = strings.ToUpper(input)
	e, err := d.Lookup(input)
	if err != nil {
		if errors.Is(err, dxcc.ErrNotFound) {
			return fmt.Errorf("the prefix %q cannot be found", input)
		}
		return err
	}
	fmt.Printf("Prefix: %s > %s = %s - Continent: %s, CQZone: %d, ITUZone: %d\n",
		input, e.Prefix, e.Country, e.Continent, e.CQZone, e.ITUZone)
	return nil
}

// doCheck reports whether the named entity exists. The match is case-insensitive
// on input but reports the canonical entity name, mirroring countries.py check.
func doCheck(d *dxcc.DXCC, name string) error {
	if canon, ok := findEntity(d, name); ok {
		fmt.Printf("Country %q found.\n", canon)
		return nil
	}
	return fmt.Errorf("the country %q cannot be found", strings.ToUpper(name))
}

// doPrefix lists all prefixes for a country, comma-joined and wrapped with a
// " >  " indent, mirroring countries.py country().
func doPrefix(d *dxcc.DXCC, name string) error {
	canon, ok := findEntity(d, name)
	if !ok {
		return fmt.Errorf("the country %q cannot be found", strings.ToUpper(name))
	}
	prefixes, err := d.GetEntity(canon)
	if err != nil {
		return err
	}
	for _, line := range wrap(strings.Join(prefixes, ", "), 70, " >  ") {
		fmt.Println(line)
	}
	return nil
}

// findEntity resolves a case-insensitive entity name to its canonical form.
func findEntity(d *dxcc.DXCC, name string) (string, bool) {
	if d.IsEntity(name) {
		return name, true
	}
	upper := strings.ToUpper(name)
	for _, e := range d.Entities() {
		if strings.ToUpper(e) == upper {
			return e, true
		}
	}
	return "", false
}

// wrap word-wraps text to width columns, prefixing every line with indent. It
// mirrors textwrap's behaviour closely enough for readable prefix listings.
func wrap(text string, width int, indent string) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{indent}
	}

	var lines []string
	line := indent
	for _, w := range words {
		if line == indent {
			line += w
			continue
		}
		if len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = indent + w
			continue
		}
		line += " " + w
	}
	lines = append(lines, line)
	return lines
}
