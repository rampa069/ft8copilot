package lotw

import (
	"bufio"
	"crypto/tls"
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Default settings, mirroring the constants in plugins/base.py.
const (
	// DefaultURL is the ARRL LOTW user-activity CSV.
	DefaultURL = "https://lotw.arrl.org/lotw-user-activity.csv"
	// DefaultPath is the on-disk cache location.
	DefaultPath = "/tmp/lotw_cache.dat"
	// DefaultExpire is how long a cache file stays fresh (7 days).
	DefaultExpire = 7 * 24 * time.Hour
	// DefaultLastSeen keeps only users seen within the last 270 days.
	DefaultLastSeen = 270 * 24 * time.Hour
)

// Cache is an in-memory set of uppercased LOTW callsigns backed by an on-disk
// cache file. Lookups are O(1); the set is loaded once at construction.
type Cache struct {
	users    map[string]struct{}
	path     string
	url      string
	expire   time.Duration
	lastSeen time.Duration

	// fetch is a seam for retrieving the activity CSV. It defaults to an
	// HTTP client with certificate verification disabled (see defaultFetch).
	// Tests may inject a fake to avoid hitting the network.
	fetch func(url string) (io.ReadCloser, error)

	// now is a seam for the current time, used by tests.
	now func() time.Time
}

// Option customizes a Cache. Unset options use the package defaults.
type Option func(*Cache)

// WithURL overrides the download URL.
func WithURL(url string) Option { return func(c *Cache) { c.url = url } }

// WithExpire overrides the cache-file expiry duration.
func WithExpire(d time.Duration) Option { return func(c *Cache) { c.expire = d } }

// WithLastSeen overrides the "users seen within" window.
func WithLastSeen(d time.Duration) Option { return func(c *Cache) { c.lastSeen = d } }

// WithFetch overrides the download seam (used in tests).
func WithFetch(fn func(url string) (io.ReadCloser, error)) Option {
	return func(c *Cache) { c.fetch = fn }
}

// WithNow overrides the clock (used in tests).
func WithNow(fn func() time.Time) Option { return func(c *Cache) { c.now = fn } }

// Default opens the cache at DefaultPath with default settings.
func Default() (*Cache, error) {
	return New(DefaultPath)
}

// New opens (and if necessary rebuilds) the LOTW cache at cachePath.
//
// If the on-disk cache is missing or older than the expiry duration, the
// activity CSV is downloaded, filtered to recently-seen callsigns, and the
// cache file is rewritten. Otherwise the existing cache is loaded as-is.
func New(cachePath string, opts ...Option) (*Cache, error) {
	c := &Cache{
		users:    make(map[string]struct{}),
		path:     cachePath,
		url:      DefaultURL,
		expire:   DefaultExpire,
		lastSeen: DefaultLastSeen,
		fetch:    defaultFetch,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}

	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// load reads the cache from disk; if it is missing or expired it rebuilds it.
func (c *Cache) load() error {
	users, stamp, err := readCacheFile(c.path)
	expired := err != nil || c.now().After(stamp.Add(c.expire))
	if !expired {
		c.users = users
		return nil
	}
	return c.rebuild()
}

// rebuild downloads the activity CSV, filters it, stores the result in memory
// and persists it to the cache file.
func (c *Cache) rebuild() error {
	rc, err := c.fetch(c.url)
	if err != nil {
		return fmt.Errorf("lotw: download: %w", err)
	}
	defer rc.Close()

	cutoff := c.now().Add(-c.lastSeen)
	users, err := parseActivity(rc, cutoff)
	if err != nil {
		return fmt.Errorf("lotw: parse: %w", err)
	}

	c.users = users
	if err := writeCacheFile(c.path, users, c.now()); err != nil {
		return fmt.Errorf("lotw: persist: %w", err)
	}
	return nil
}

// Contains reports whether call (case-insensitive) is a known LOTW user.
func (c *Cache) Contains(call string) bool {
	_, ok := c.users[strings.ToUpper(strings.TrimSpace(call))]
	return ok
}

// Len returns the number of callsigns in the cache.
func (c *Cache) Len() int { return len(c.users) }

// parseActivity reads "CALLSIGN,YYYY-MM-DD,HH:MM:SS" lines and returns the set
// of uppercased callsigns whose date is at or after cutoff. Malformed lines are
// skipped silently, matching the upstream's tolerant behavior.
func parseActivity(r io.Reader, cutoff time.Time) (map[string]struct{}, error) {
	const dateLayout = "2006-01-02"
	users := make(map[string]struct{})

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		call := strings.TrimSpace(fields[0])
		if call == "" {
			continue
		}
		seen, err := time.Parse(dateLayout, strings.TrimSpace(fields[1]))
		if err != nil {
			continue
		}
		if seen.Before(cutoff) {
			continue
		}
		users[strings.ToUpper(call)] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// cacheFile is the gob-encoded on-disk representation.
type cacheFile struct {
	Stamp time.Time
	Users []string
}

// readCacheFile loads the persisted set and its timestamp.
func readCacheFile(path string) (map[string]struct{}, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()

	var cf cacheFile
	if err := gob.NewDecoder(f).Decode(&cf); err != nil {
		return nil, time.Time{}, err
	}
	users := make(map[string]struct{}, len(cf.Users))
	for _, u := range cf.Users {
		users[u] = struct{}{}
	}
	return users, cf.Stamp, nil
}

// writeCacheFile persists the set together with stamp.
func writeCacheFile(path string, users map[string]struct{}, stamp time.Time) error {
	cf := cacheFile{Stamp: stamp, Users: make([]string, 0, len(users))}
	for u := range users {
		cf.Users = append(cf.Users, u)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(cf); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// defaultFetch downloads url over TLS with certificate verification disabled.
//
// Certificate verification is intentionally disabled to match upstream
// FT8Commander (plugins/base.py uses ssl._create_unverified_context()). The
// ARRL endpoint has historically served a chain that fails default
// verification, so the Python app skips it; we replicate that here.
// #nosec G402 -- InsecureSkipVerify mirrors upstream behavior, see above.
func defaultFetch(url string) (io.ReadCloser, error) {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download error: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}
