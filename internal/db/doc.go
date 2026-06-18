// Package db is the SQLite persistence layer for the cqcalls table: upsert,
// status update, delete, purge and queries. A goroutine consumes DB commands
// from a channel (replacing the Python thread+Queue design).
//
// Port of dbutils.py. See FT8CoPilot-rxn.6.
package db
