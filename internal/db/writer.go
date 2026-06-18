package db

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rampamac/ft8copilot/internal/dxcc"
	"github.com/rampamac/ft8copilot/internal/geo"
)

// Spot is the raw payload of an Insert command: a station heard calling CQ,
// before geo/DXCC enrichment.
type Spot struct {
	Call      string
	Extra     string
	Grid      string
	Frequency uint64
	Band      int
	Packet    Packet
}

// Command is a database mutation delivered to the Writer over a channel,
// replacing the (DBCommand, data) tuples the Python version pushed onto a Queue.
type Command interface{ isCommand() }

// InsertCmd records a station heard calling CQ (DBCommand.INSERT).
type InsertCmd struct{ Spot Spot }

// StatusCmd updates a row's status (DBCommand.STATUS): 1 = being worked,
// 2 = logged/worked.
type StatusCmd struct {
	Call   string
	Band   int
	Status int
}

// DeleteCmd removes an in-progress row (DBCommand.DELETE) when the station
// answered someone else.
type DeleteCmd struct {
	Call string
	Band int
}

func (InsertCmd) isCommand() {}
func (StatusCmd) isCommand() {}
func (DeleteCmd) isCommand() {}

const insertSQL = `
INSERT INTO cqcalls VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(call, band) DO UPDATE SET snr = excluded.snr, packet = excluded.packet
WHERE status <> 2`

const updateSQL = "UPDATE cqcalls SET status = ? WHERE status <> 2 AND call = ? AND band = ?"

const deleteSQL = "DELETE FROM cqcalls WHERE status = 1 AND call = ? AND band = ?"

// Writer applies Commands to the Store. It owns the geo origin (the operator's
// grid) and the DXCC lookup used to enrich Insert commands. A single Writer
// goroutine should consume the command channel; concurrent readers may query
// the Store independently.
type Writer struct {
	store    *Store
	origin   geo.Point
	entities *dxcc.DXCC
	log      *slog.Logger
}

// NewWriter creates a Writer. myGrid is the operator's Maidenhead locator (used
// as the origin for distance/azimuth). entities is the DXCC resolver. If log is
// nil the default logger is used.
func NewWriter(store *Store, myGrid string, entities *dxcc.DXCC, log *slog.Logger) (*Writer, error) {
	origin, err := geo.GridToLatLon(myGrid)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	return &Writer{store: store, origin: origin, entities: entities, log: log}, nil
}

// Run consumes commands until the channel is closed or the context is cancelled.
func (w *Writer) Run(ctx context.Context, cmds <-chan Command) {
	w.log.Info("database writer started")
	for {
		select {
		case <-ctx.Done():
			return
		case cmd, ok := <-cmds:
			if !ok {
				return
			}
			if err := w.Process(cmd); err != nil {
				w.log.Error("db command failed", "err", err)
			}
		}
	}
}

// Process applies a single command synchronously. It returns an error only for
// genuine database failures; expected skips (unknown DXCC entity, row already
// worked) are logged and return nil.
func (w *Writer) Process(cmd Command) error {
	switch c := cmd.(type) {
	case InsertCmd:
		return w.insert(c.Spot)
	case StatusCmd:
		_, err := w.store.db.Exec(updateSQL, c.Status, c.Call, c.Band)
		return err
	case DeleteCmd:
		_, err := w.store.db.Exec(deleteSQL, c.Call, c.Band)
		return err
	default:
		w.log.Warn("unknown db command", "type", cmdType(cmd))
		return nil
	}
}

func (w *Writer) insert(spot Spot) error {
	point, err := geo.GridToLatLon(spot.Grid)
	if err != nil {
		w.log.Warn("invalid grid, skipping", "call", spot.Call, "grid", spot.Grid)
		return nil
	}
	entity, err := w.entities.Lookup(spot.Call)
	if err != nil {
		// Matches the original: an unresolvable callsign is almost certainly a
		// fake/garbled call, so we skip it.
		w.log.Error("DXCC lookup failed, probably a fake callsign", "call", spot.Call)
		return nil
	}

	packetJSON, err := json.Marshal(spot.Packet)
	if err != nil {
		return err
	}

	res, err := w.store.db.Exec(insertSQL,
		spot.Call,
		spot.Extra,
		spot.Packet.Time.UTC().Format(timeLayout),
		0, // status
		spot.Packet.SNR,
		spot.Grid,
		point.Lat,
		point.Lon,
		geo.Distance(w.origin, point),
		geo.Azimuth(w.origin, point),
		entity.Country,
		entity.Continent,
		entity.CQZone,
		entity.ITUZone,
		int64(spot.Frequency),
		spot.Band,
		string(packetJSON),
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		w.log.Debug("already worked", "call", spot.Call, "band", spot.Band)
	} else {
		w.log.Debug("stored", "call", spot.Call, "country", entity.Country, "grid", spot.Grid)
	}
	return nil
}

func cmdType(cmd Command) string {
	switch cmd.(type) {
	case InsertCmd:
		return "insert"
	case StatusCmd:
		return "status"
	case DeleteCmd:
		return "delete"
	default:
		return "unknown"
	}
}
