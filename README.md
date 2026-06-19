# FT8CoPilot

> Experimental. A Go port of [FT8Commander](https://github.com/0x9900/FT8Commander)
> by Fred W6BSD. Works with WSJT-X 2.5 and above.

## WSJT-X FT8 Automation

FT8CoPilot automates FT8/FT4 contacts by controlling WSJT-X over its UDP
protocol. After each receive sequence it scores the stations calling CQ — using
SNR, distance, and a configurable chain of selector plugins — and automatically
replies to the one with the best chance of completing the QSO. It is meant for
contesting and DX hunting, where you want to make as many QSOs as possible.

Runs on macOS, Linux and Windows. All four commands are statically linked,
pure-Go binaries (no cgo, no SQLite system library required).

## How it works

```
            UDP                      channel                 SQLite
 WSJT-X  ─────────►  ft8ctrl  ───────────────►  db.Writer  ────────►  cqcalls
   ▲      Decode/      │ Sequencer    InsertCmd   (DXCC +              (one row
   │      Status/      │              StatusCmd    geo enrich)         per call
   │      Logged       │              DeleteCmd                        + band)
   │                   │
   └───────────────────┘  Reply / HaltTx
       at each FT8/FT4 sequence boundary, the selector chain picks the
       best station from cqcalls and ft8ctrl tells WSJT-X to answer it.
```

- **`ft8ctrl`** — the automation daemon (port of `ft8ctrl.py`).
- **`lookup`** — inspect the `cqcalls` database (port of `lookup.py`).
- **`countries`** — DXCC entity lookup helper (port of `countries.py`).
- **`adif`** — import/export the `cqcalls` database in ADIF format.

## Install

Requires Go 1.26+.

```sh
make build      # builds bin/ft8ctrl, bin/lookup, bin/countries
make test       # run the unit + integration tests
make lint       # golangci-lint (optional)
make release    # static binaries for linux/darwin/windows ({amd64,arm64}) -> dist/
```

Or install a single command directly:

```sh
go install github.com/rampa069/ft8copilot/cmd/ft8ctrl@latest
```

## Usage

1. Start WSJT-X and enable its UDP reporting (Settings → Reporting → UDP Server,
   default `127.0.0.1:2237`; set the port to match `wsjt_port` below).
2. Copy the sample config and edit it:
   ```sh
   cp ft8ctrl.yaml.sample ft8ctrl.yaml
   $EDITOR ft8ctrl.yaml          # set my_call, my_grid, and the selectors
   ```
3. Run the daemon:
   ```sh
   bin/ft8ctrl -c ft8ctrl.yaml
   ```
4. Watch WSJT-X make contacts. Stop with Ctrl-C.

### Interactive TUI

Run with `--tui` for an interactive terminal front-end (retro DOS look) instead
of plain log output:

```sh
bin/ft8ctrl --tui -c ft8ctrl.yaml
```

The automation runs exactly as in headless mode — the sequencer keeps driving
WSJT-X — while the UI shows live state and lets you steer it. Layout:

```
 FT8 CoPilot — EA5IUE
╔═╡ Candidates · 20m ╞═══════════════╗╔═╡ Status ╞═══════════╗
║▶  OK1KKI   -3  1450 15 Czech Rep.  ║║Auto     ● RUNNING    ║
║   G3XYZ    -6  1300 14 England     ║║Band     20m          ║
╚════════════════════════════════════╝╚══════════════════════╝
╔═╡ Log ╞════════════════════════════════════════════════════╗
║11:02:44 INFO  calling call=OK1KKI country="Czech Republic" ║
╚════════════════════════════════════════════════════════════╝
 F1 Help  F2 Pause  F3 Search  F4 Params  F5 Cands  F10 Quit
```

- **Candidates panel** — the band's spots ranked best-to-worst; the station the
  autopilot would call next is marked `▶`.
- **Status panel** — your call, band/frequency, autopilot `● RUNNING` /
  `■ PAUSED`, the station being worked, and spot counts.
- **Log window** — the live daemon log (the rotating debug file still captures
  everything).

Key bindings (also shown on the **F1** help screen):

| key | action |
|-----|--------|
| `F1` | help |
| `F2` / `Space` | pause / resume the autopilot (ingestion keeps running) |
| `F3` | search the call database (by call / country / grid) |
| `F4` | edit live parameters (`tx_power`, `tx_retries`, `follow_frequency`, `retry_time`, the selector chain, the blacklist) |
| `F5` | full-screen ranked candidates view |
| `F10` / `q` | quit |

Parameter edits made in **F4** apply live through the same path as a `SIGHUP`
reload (they are **not** written back to `ft8ctrl.yaml`; edit the file or send
`SIGHUP` to persist). Identity/socket/database fields cannot be changed at
runtime.

#### Reloading the configuration without restarting

Send `SIGHUP` to reload `ft8ctrl.yaml` in place — no need to stop the daemon or
lose the UDP/database state:

```sh
kill -HUP $(pgrep ft8ctrl)
```

Applied live: the `call_selector` chain and every selector section, `BlackList`,
`retry_time`, `tx_retries`, `tx_power` and `follow_frequency`. The change takes
effect on the next FT8/FT4 sequence. If the new file fails to parse (or a
selector fails to build) the daemon logs the error and keeps running with the
previous configuration.

Requires a restart (a warning is logged if changed, and the value is ignored):
`my_call`, `my_grid`, `db_name`, `wsjt_ip`, `wsjt_port`, `logger_ip`,
`logger_port`, `logfile_name` — these own the sockets, the database or the log
file, which are opened once at startup.

The config file is searched in `/etc`, `~/.local/etc`, and `.` (in that order)
when `-c` is omitted. Set `LOG_LEVEL=DEBUG` for verbose console output; a rotating
debug log is always written to `logfile_name` (default `ft8ctrl-debug.log`).

The console output is colourised by level (and key fields like `call` are
highlighted) when stderr is a terminal. Colour is disabled automatically when the
output is redirected to a file or pipe, or when `NO_COLOR` is set; force it either
way with `FT8_COLOR=always|never`. The rotating debug log is always plain text.

### `lookup` — database viewer

```sh
bin/lookup -c ft8ctrl.yaml --run            # live refreshing table (every 15s)
bin/lookup -c ft8ctrl.yaml --call '^CO'     # rows whose call matches a regexp
bin/lookup -c ft8ctrl.yaml --country Cuba   # rows for a country
bin/lookup -c ft8ctrl.yaml --status 2       # worked stations
bin/lookup -c ft8ctrl.yaml -d CO8LY -b 20   # delete a call on a band
```

Add `-b/--band <meters>` to filter by band. The `lotw` column shows whether each
station is a registered LOTW user.

### `countries` — DXCC helper

```sh
bin/countries -l                 # list all DXCC entities
bin/countries -c W6BSD           # resolve a callsign/prefix -> country, zones
bin/countries -C Cuba            # check an entity exists
bin/countries -p Cuba            # list all prefixes for an entity
```

### `adif` — import / export the database

```sh
bin/adif -c ft8ctrl.yaml import mylog.adi             # import, marking QSOs worked
bin/adif -c ft8ctrl.yaml import --dry-run mylog.adi   # parse and report only
bin/adif -c ft8ctrl.yaml export worked.adi            # export worked rows
bin/adif -c ft8ctrl.yaml export --all --band 20 -     # filtered export to stdout
```

**Import.** The "worked" status (`status = 2`) is normally set only when WSJT-X
logs a live QSO, so a fresh database has no history: the `DXCC100` selector and
already-worked filtering have nothing to go on until you make contacts. Importing
an ADIF export (WSJT-X, QRZ, LoTW, …) seeds that history. Each record is upserted
into `cqcalls` keyed by `(call, band)` with status "worked", enriched via the
same DXCC lookup the selectors use (falling back to the ADIF `COUNTRY`/`CQZ`/
`CONT` fields when a call can't be resolved). The band comes from the ADIF `BAND`
field (`20m` → 20m) or `FREQ` as a fallback; a missing grid is fine. Re-running is
idempotent. The summary reports imported / skipped / per-band:

```
parsed 7908 records
imported 7907 QSOs (marked worked)
skipped 1 (no band=1)
by band: 80m=13  40m=885  30m=543  20m=4507  17m=415  15m=809  12m=120 ...
```

**Export.** Writes `cqcalls` rows as ADIF records (with a header and your
`station_callsign`/`my_gridsquare` from the config). By default only worked rows
are exported; `--all` exports every row, and `--band N` / `--status N` filter.
Use `-` as the file to write to stdout (the summary then goes to stderr).

## Configuration

See `ft8ctrl.yaml.sample` for a fully commented example. The `ft8ctrl` section:

| key | meaning |
|-----|---------|
| `my_call` / `my_grid` | your callsign and Maidenhead grid (origin for distances) |
| `my_continent` | your own continent (NA/EU/AS/…), used to skip stations calling `CQ DX` from your own continent; auto-derived from `my_call` when omitted |
| `db_name` | SQLite database path (`~` is expanded) |
| `wsjt_ip` / `wsjt_port` | where to listen for WSJT-X UDP (default `127.0.0.1:2238`) |
| `follow_frequency` | send replies on the caller's audio frequency (SHIFT modifier) |
| `retry_time` | minutes after which an un-worked spot is purged |
| `tx_power` | power (W) stamped onto QSOs forwarded to a secondary logger |
| `tx_retries` | how many transmit cycles to repeat a message before giving up |
| `call_selector` | the ordered list of selectors to try (first match wins) |
| `logger_ip` / `logger_port` | optional secondary logging app to forward QSOs to |

### Selector plugins

`call_selector` lists the selectors to run in order; the first to return a
station wins. Each selector reads its own same-named config section. Common
options on every selector: `min_snr`, `max_snr`, `lotw_users_only` (only answer
registered LOTW users), and `reverse: True` (invert the list/regexp, i.e. "not
in"). The top-level `BlackList` list is always skipped.

| selector | section keys | selects |
|----------|--------------|---------|
| `Any` | — | any caller passing the SNR/blacklist/LOTW filters |
| `CallSign` | `regexp`, `list`, `reverse` | calls matching a regexp or in a list |
| `Grid` | `regexp`, `reverse` | calls whose grid matches a regexp |
| `Continent` | `list`, `reverse` | continents (AF/AS/EU/NA/OC/SA) |
| `Country` | `list`, `reverse` | DXCC entities by name |
| `CQZone` | `list`, `reverse` | CQ zones (integers) |
| `ITUZone` | `list`, `reverse` | ITU zones (integers) |
| `DXCC100` | `worked_count` | only countries not yet worked `worked_count` times on the band |
| `Extra` | `list`, `reverse` | the tag after `CQ` (e.g. `POTA`, `DX`) |

> Country names must match the DXCC database exactly — e.g. Germany is
> `Fed. Rep. of Germany`. Use `bin/countries -c <call>` to see the canonical name.

The selector picks the highest-SNR station among those passing its filter (a
distance/SNR coefficient is also computed and attached to each candidate).

## Notes

### Logging contacts automatically (macOS)

WSJT-X pops a "Log QSO" dialog after each contact. This AppleScript clicks **Ok**
for you:

```applescript
tell application "wsjtx" to activate
tell application "System Events"
    repeat
        try
            tell application process "WSJT-X" to set winList to every window
            repeat with win in winList
                if name of win contains "Log QSO" then
                    tell application process "WSJT-X" to click button "Ok" of group 1 of win
                    say "Logged"
                end if
            end repeat
        on error errMsg
            log errMsg
        end try
        delay 3
    end repeat
end tell
```

A more complete "call CQ and auto-log" script by JC (W6IPA) is available in the
[original gist](https://gist.github.com/jc-m/f4ae181cdbac7adc8621e93a0c26c8e5).

## Differences from the Python original

This is a faithful port, with a few deliberate, documented changes:

- The DXCC lookup is a self-contained Go parser over an embedded `cty.dat`
  (replacing the Python `DXEntity` package).
- The database writer is a goroutine consuming a command channel (replacing the
  Python thread + `Queue`); the selector pool is cached per band.
- A couple of latent bugs in the Python `zones.py` (infinite recursion and an
  int-vs-string comparison that never matched) are fixed; the CQ/ITU zone
  selectors actually work here.
- The stored `packet` JSON uses a standard timestamp rather than the Python
  custom datetime codec (the database is recreated by this port).

## License

BSD 3-Clause. Original work © Fred W6BSD; this Go port retains the same license
and attribution. See `LICENSE`.
