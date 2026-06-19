// Package config loads the ft8ctrl.yaml configuration file into typed structs.
//
// This is a Go port of FT8Commander's config.py. The Python code uses a
// dynamic singleton that exposes YAML sections as attribute-bearing objects
// and supports dotted lookups (e.g. config['ft8ctrl.db_name']). Here we replace
// that with a plain, typed Config struct populated by gopkg.in/yaml.v3.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// configFilename is the file searched for by Find.
const configFilename = "ft8ctrl.yaml"

// configLocations are searched, in order, by Find. The "~/.local/etc" entry has
// its leading ~ expanded to the user's home directory.
var configLocations = []string{"/etc", "~/.local/etc", "."}

// Config is the fully parsed ft8ctrl configuration.
type Config struct {
	// FT8Ctrl holds the main "ft8ctrl" section.
	FT8Ctrl FT8Ctrl `yaml:"ft8ctrl"`

	// BlackList holds the top-level "BlackList" callsign list. Callsigns are
	// upper-cased on load, matching BlackList in plugins/base.py.
	BlackList []string `yaml:"BlackList"`

	// Selectors holds the per-selector plugin configuration sections (Any,
	// Grid, CallSign, CQZone, ITUZone, Country, Continent, DXCC100, Extra).
	Selectors map[string]SelectorConfig `yaml:"-"`
}

// FT8Ctrl mirrors the "ft8ctrl" section of the YAML file.
type FT8Ctrl struct {
	MyCall          string     `yaml:"my_call"`
	MyGrid          string     `yaml:"my_grid"`
	MyContinent     string     `yaml:"my_continent"` // own continent (NA/EU/AS/…); derived from my_call when empty
	DBName          string     `yaml:"db_name"` // ~ is expanded on load
	WSJTIP          string     `yaml:"wsjt_ip"`
	WSJTPort        int        `yaml:"wsjt_port"`
	FollowFrequency bool       `yaml:"follow_frequency"`
	RetryTime       int        `yaml:"retry_time"` // minutes
	TXPower         int        `yaml:"tx_power"`
	TXRetries       int        `yaml:"tx_retries"` // defaults to 5 when absent
	CallSelector    StringList `yaml:"call_selector"`

	// Optional remote logger (UDP forwarding) settings.
	LoggerIP   string `yaml:"logger_ip"`
	LoggerPort int    `yaml:"logger_port"`

	// Optional log file path override.
	LogfileName string `yaml:"logfile_name"`
}

// SelectorConfig holds the per-selector keys read via getattr in the Python
// selector plugins (plugins/*.py). Every key is optional. min_snr / max_snr are
// pointers so callers can distinguish "unset" from a configured value: the
// Python base class applies MIN_SNR=-50 / MAX_SNR=+50 defaults when unset, but
// we keep those defaults out of config and leave them to callers.
type SelectorConfig struct {
	MinSNR        *int       `yaml:"min_snr"`
	MaxSNR        *int       `yaml:"max_snr"`
	LOTWUsersOnly bool       `yaml:"lotw_users_only"`
	Reverse       bool       `yaml:"reverse"`
	Regexp        string     `yaml:"regexp"`
	List          StringList `yaml:"list"`
	WorkedCount   int        `yaml:"worked_count"`
	Debug         bool       `yaml:"debug"`
	Delta         int        `yaml:"delta"`
	MyContinent   string     `yaml:"my_continent"`
}

// selectorNames are the recognized per-selector section keys.
var selectorNames = []string{
	"Any", "Grid", "CallSign", "CQZone", "ITUZone",
	"Country", "Continent", "DXCC100", "Extra",
}

// StringList is a YAML list whose scalar elements may be strings or integers,
// all stored as their string form. It also accepts a single scalar, which is
// treated as a one-element list. This mirrors the Python selectors, where a
// lone string is wrapped (`[x] if isinstance(x, str) else x`) and ZoneSelector
// converts integers with `str(int(zone))`.
type StringList []string

// UnmarshalYAML accepts either a scalar (one element) or a sequence of scalars,
// converting each scalar node to its string representation.
func (s *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*s = StringList{node.Value}
		return nil
	case yaml.SequenceNode:
		out := make(StringList, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("config: list element at line %d is not a scalar", item.Line)
			}
			out = append(out, item.Value)
		}
		*s = out
		return nil
	default:
		return fmt.Errorf("config: cannot decode %v into a string list", node.Tag)
	}
}

// Load reads and parses the configuration file at the given path. A leading ~
// in path is expanded to the user's home directory, as is db_name.
func Load(path string) (*Config, error) {
	expanded, err := expandUser(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("config: reading %q: %w", expanded, err)
	}

	return parse(data)
}

// Find searches /etc, ~/.local/etc and . (in that order) for ft8ctrl.yaml and
// loads the first one found. It returns an error if no file is found.
func Find() (*Config, error) {
	path, err := FindFile()
	if err != nil {
		return nil, err
	}
	return Load(path)
}

// FindFile returns the path of the first ft8ctrl.yaml found in the standard
// locations (/etc, ~/.local/etc, .), without loading it. It returns an error if
// none is found. Callers that need to reload the same file later (e.g. on
// SIGHUP) resolve the path once with FindFile and reload it with Load.
func FindFile() (string, error) {
	for _, loc := range configLocations {
		dir, err := expandUser(loc)
		if err != nil {
			return "", err
		}
		candidate := filepath.Join(dir, configFilename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("config: %s not found in %s", configFilename, strings.Join(configLocations, ", "))
}

// parse unmarshals the raw YAML bytes into a Config and applies post-processing
// (defaults, ~ expansion, blacklist upper-casing, selector extraction).
func parse(data []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse error: %w", err)
	}

	// Pull the per-selector sections out of the document into cfg.Selectors.
	var sections map[string]yaml.Node
	if err := yaml.Unmarshal(data, &sections); err != nil {
		return nil, fmt.Errorf("config: parse error: %w", err)
	}
	cfg.Selectors = make(map[string]SelectorConfig)
	for _, name := range selectorNames {
		node, ok := sections[name]
		if !ok || node.IsZero() {
			continue
		}
		var sel SelectorConfig
		if err := node.Decode(&sel); err != nil {
			return nil, fmt.Errorf("config: section %q: %w", name, err)
		}
		cfg.Selectors[name] = sel
	}

	// tx_retries defaults to 5 when absent (Python: getattr(..., 5)).
	if cfg.FT8Ctrl.TXRetries == 0 {
		cfg.FT8Ctrl.TXRetries = 5
	}

	// Expand ~ in db_name.
	if cfg.FT8Ctrl.DBName != "" {
		expanded, err := expandUser(cfg.FT8Ctrl.DBName)
		if err != nil {
			return nil, err
		}
		cfg.FT8Ctrl.DBName = expanded
	}

	// Upper-case the blacklist, matching plugins/base.py.
	for i, call := range cfg.BlackList {
		cfg.BlackList[i] = strings.ToUpper(call)
	}

	return cfg, nil
}

// expandUser expands a leading ~ (or ~/) in path to the user's home directory.
func expandUser(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot expand %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
