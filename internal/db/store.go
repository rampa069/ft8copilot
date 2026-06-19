package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // SQLite driver ("sqlite")
)

// timeLayout is how the cqcalls.time column is stored. It matches SQLite's own
// datetime() text format so the Purge comparison against datetime('now', ...)
// works with simple lexicographic ordering. Times are stored in UTC.
const timeLayout = "2006-01-02 15:04:05"

const schema = `
CREATE TABLE IF NOT EXISTS cqcalls
(
  call TEXT,
  extra TEXT,
  time TIMESTAMP,
  status INTEGER,
  snr INTEGER,
  grid TEXT,
  lat REAL,
  lon REAL,
  distance REAL,
  azimuth REAL,
  country TEXT,
  continent TEXT,
  cqzone INTEGER,
  ituzone INTEGER,
  frequency INTEGER,
  band INTEGER,
  packet JSON
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_call on cqcalls (call, band);
CREATE INDEX IF NOT EXISTS idx_time on cqcalls (time DESC);
CREATE INDEX IF NOT EXISTS idx_grid on cqcalls (grid ASC);
`

// columns lists the cqcalls columns in schema order, used by SELECTs.
const columns = "call, extra, time, status, snr, grid, lat, lon, distance, " +
	"azimuth, country, continent, cqzone, ituzone, frequency, band, packet"

// Record is a full cqcalls row.
type Record struct {
	Call      string
	Extra     string
	Time      time.Time
	Status    int
	SNR       int32
	Grid      string
	Lat       float64
	Lon       float64
	Distance  float64
	Azimuth   int
	Country   string
	Continent string
	CQZone    int
	ITUZone   int
	Frequency uint64
	Band      int
	Packet    Packet
}

// Store is the SQLite persistence layer for the cqcalls table. It is safe for a
// single writer goroutine (see Writer) plus concurrent readers; WAL mode and a
// busy timeout are enabled to make that work.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and ensures
// the cqcalls schema exists.
func Open(path string) (*Store, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}
	for _, pragma := range []string{
		"PRAGMA busy_timeout=15000;",
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("db: %s: %w", pragma, err)
		}
	}
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: create schema: %w", err)
	}
	return &Store{db: conn}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// GetCall returns the first row for the given callsign (any band). The bool is
// false when no row exists. Port of get_call in dbutils.py.
func (s *Store) GetCall(call string) (Record, bool, error) {
	row := s.db.QueryRow("SELECT "+columns+" FROM cqcalls WHERE call = ?", call)
	rec, err := scanRecord(row)
	if err == sql.ErrNoRows {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	return rec, true, nil
}

// Recent returns the unworked spots (status = 0) on a band that were seen after
// since, oldest first. This is the base query used by the call selectors.
func (s *Store) Recent(band int, since time.Time) ([]Record, error) {
	rows, err := s.db.Query(
		"SELECT "+columns+" FROM cqcalls WHERE status = 0 AND band = ? AND time > ? ORDER BY time ASC",
		band, since.UTC().Format(timeLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// WorkedCountries returns the countries already worked (status = 2) on a band at
// least minCount times. Used by the DXCC100 selector.
func (s *Store) WorkedCountries(band, minCount int) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT country FROM cqcalls WHERE status = 2 AND band = ? GROUP BY country HAVING count(*) >= ?",
		band, minCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCallBand removes every row for a callsign on a band regardless of
// status and reports how many rows were removed. Used by the lookup CLI.
func (s *Store) DeleteCallBand(call string, band int) (int64, error) {
	res, err := s.db.Exec("DELETE FROM cqcalls WHERE call = ? AND band = ?", call, band)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FindByStatus returns rows with the given status, optionally filtered by band
// (nil means no band filter), oldest first. Used by the lookup CLI's --status.
func (s *Store) FindByStatus(status int, band *int) ([]Record, error) {
	query := "SELECT " + columns + " FROM cqcalls WHERE status = ?"
	args := []any{status}
	if band != nil {
		query += " AND band = ?"
		args = append(args, *band)
	}
	query += " ORDER BY time ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// FindByCountry returns rows for the given country, optionally filtered by band
// (nil means no band filter), oldest first. Used by the lookup CLI's --country.
func (s *Store) FindByCountry(country string, band *int) ([]Record, error) {
	query := "SELECT " + columns + " FROM cqcalls WHERE country = ?"
	args := []any{country}
	if band != nil {
		query += " AND band = ?"
		args = append(args, *band)
	}
	query += " ORDER BY time ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// All returns every row, optionally filtered by band (nil means no band
// filter), oldest first. The lookup CLI uses this for the --call regexp filter,
// applying the regular expression in Go rather than in SQLite.
func (s *Store) All(band *int) ([]Record, error) {
	query := "SELECT " + columns + " FROM cqcalls"
	var args []any
	if band != nil {
		query += " WHERE band = ?"
		args = append(args, *band)
	}
	query += " ORDER BY time ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// Since returns rows seen within the last d (time > now-d), oldest first. Used
// by the lookup CLI's --run refreshing view.
func (s *Store) Since(d time.Duration) ([]Record, error) {
	cutoff := time.Now().UTC().Add(-d)
	rows, err := s.db.Query(
		"SELECT "+columns+" FROM cqcalls WHERE time > ? ORDER BY time ASC",
		cutoff.Format(timeLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// Query describes a flexible cqcalls search for the TUI search modal. Every
// field is optional; set fields are ANDed together. Text matches the call,
// country or grid (case-insensitive substring).
type Query struct {
	Text   string // OR-matched against call, country and grid (LIKE %text%)
	Status *int   // exact status (0 unworked, 1 in progress, 2 worked)
	Band   *int   // exact band in metres
	Limit  int    // cap on rows returned (0 = no limit)
}

// Search runs a Query and returns the matching rows, most recent first. It is a
// read-only helper for the TUI; the LIKE clauses lean on SQLite's
// case-insensitive ASCII matching, so callers need not normalise case.
func (s *Store) Search(q Query) ([]Record, error) {
	query := "SELECT " + columns + " FROM cqcalls WHERE 1=1"
	var args []any

	if t := strings.TrimSpace(q.Text); t != "" {
		like := "%" + t + "%"
		query += " AND (call LIKE ? OR country LIKE ? OR grid LIKE ?)"
		args = append(args, like, like, like)
	}
	if q.Status != nil {
		query += " AND status = ?"
		args = append(args, *q.Status)
	}
	if q.Band != nil {
		query += " AND band = ?"
		args = append(args, *q.Band)
	}
	query += " ORDER BY time DESC"
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// Purge removes stale unworked rows (status < 2) older than retry. Port of the
// Purge thread's query in dbutils.py. Returns the number of rows removed.
func (s *Store) Purge(retry time.Duration) (int64, error) {
	mins := int(retry.Minutes())
	if mins < 0 {
		mins = -mins
	}
	modifier := fmt.Sprintf("-%d minutes", mins)
	res, err := s.db.Exec(
		"DELETE FROM cqcalls WHERE status < 2 AND time < datetime('now', ?)", modifier)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(s scanner) (Record, error) {
	var (
		rec     Record
		timeStr string
		azimuth float64
		freq    int64
		packet  string
	)
	err := s.Scan(
		&rec.Call, &rec.Extra, &timeStr, &rec.Status, &rec.SNR, &rec.Grid,
		&rec.Lat, &rec.Lon, &rec.Distance, &azimuth, &rec.Country, &rec.Continent,
		&rec.CQZone, &rec.ITUZone, &freq, &rec.Band, &packet,
	)
	if err != nil {
		return Record{}, err
	}
	rec.Azimuth = int(azimuth)
	rec.Frequency = uint64(freq)
	if t, perr := time.ParseInLocation(timeLayout, timeStr, time.UTC); perr == nil {
		rec.Time = t
	} else if t, perr := time.Parse(time.RFC3339, timeStr); perr == nil {
		// The modernc.org/sqlite driver returns TIMESTAMP-affinity columns in
		// RFC3339 form (e.g. "2026-06-18T12:00:00Z") rather than the stored
		// "2006-01-02 15:04:05" text layout, so accept that too.
		rec.Time = t.UTC()
	}
	if packet != "" {
		_ = json.Unmarshal([]byte(packet), &rec.Packet)
	}
	return rec, nil
}

func scanRecords(rows *sql.Rows) ([]Record, error) {
	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
