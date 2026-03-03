# Andon v4

Real-time visual factory monitoring system. Connects to PLC data via [Warlink](https://github.com/yatesdr/warlink) bridge, decodes machine state bitfields, and renders live andon boards on wall-mounted displays.

## Quick Start

Download the latest binary for your platform from [Releases](https://github.com/yatesdr/andonv4/releases):

| Platform | Binary |
|----------|--------|
| Linux (x86_64) | `andon-linux-amd64` |
| Linux (ARM64) | `andon-linux-arm64` |
| macOS (Intel) | `andon-darwin-amd64` |
| macOS (Apple Silicon) | `andon-darwin-arm64` |
| Windows (x86_64) | `andon-windows-amd64.exe` |

```bash
chmod +x andon-linux-amd64
./andon-linux-amd64
```

Server starts on `https://localhost:8090`. A self-signed TLS certificate is auto-generated on first run. Configuration is stored at `~/.andon/config.json` and event data in `~/.andon/andon.db` (SQLite).

### Build from Source

Requires Go 1.21+.

```bash
git clone https://github.com/yatesdr/andonv4.git
cd andonv4
go build ./cmd/andon
./andon
```

Cross-compile for all platforms with `make all` (outputs to `build/`).

### Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `8090` | HTTPS listen port (also reads `PORT` env var) |
| `-config` | `~/.andon/config.json` | Path to config file |

Port+1 (e.g. 8091) serves HTTP and redirects to HTTPS.

### Subcommands

#### `restore`

Restore configuration and database from a backup source. See [docs/backup-restore.md](docs/backup-restore.md) for full details.

```bash
andon restore --source s3 --station <id> --s3-endpoint <endpoint> --s3-bucket <bucket> [options]
andon restore --source central --station <id> --central-url <url>
```

### Prerequisites

- [Warlink](https://github.com/yatesdr/warlink) **>= 0.2.16** -- required for the SSE streaming capability that Andon v4 depends on for live PLC data.

## Setup

### 1. First Login

Navigate to the dashboard at `/`. Click the user icon (top-right) to log in. The default password is `admin` -- change it immediately under **Settings > Change Password**.

### 2. Configure Station

Go to **Settings** and fill in:

- **Station Name** -- human-readable identifier (e.g. "Plant 1 Assembly")
- **Station ID** -- machine-readable identifier (e.g. "STN-001")
- **Warlink Server URL** -- base URL of your Warlink PLC bridge (e.g. `http://192.168.1.100:8080`). Use the **Test** button to verify connectivity.

### 3. Define Shifts

Under **Settings > Shifts**, add your production shifts. Each shift has:

- **Name** -- e.g. "Day", "Afternoon", "Night"
- **Start / End** -- 24-hour times (overnight shifts like 23:00-07:00 are supported)
- **Planned minutes per hour** -- click any value to adjust for breaks or planned downtime

### 4. Create a Screen

From the dashboard, click **+ Add Screen**. This opens the **Designer** where you lay out process cells, presses, buffers, arrows, headers, and images on a 1920x1080 canvas.

### 5. Assign PLC Tags

In the designer, select a process or press shape and configure its PLC connection:

- **PLC** -- controller name as registered in Warlink
- **State Tag** -- DINT tag containing the machine state bitfield
- **Count Tag** -- DINT tag containing part counts
- **Buffer Tag** -- DINT tag containing buffer levels
- **Style Tag** -- DINT tag containing current job style
- **Coil Tag** -- tag containing coil percentage (presses only)

### 6. Set Production Targets

Click **Targets** on a screen card to configure per-cell parameters:

- **Process**: Man Time, Machine Time, Takt Time (seconds)
- **Press**: JPH (jobs/hour), Target Die Change (minutes)

Targets can vary by job style when a style tag is configured.

### 7. Display on TV

Open `/screens/{slug}` on your wall-mounted display. The page renders full-screen at 1920x1080 and auto-scales to fit the viewport. No login required for display pages.

## Features

### Dashboard Controls

- **Blackout** -- blanks all displays (shows company logo)
- **Set Status** -- overlays a message on all screens (LUNCH, MAINTENANCE, custom text, etc.)
- **Andon Toggle** -- master on/off; starting clears counts for a new production period
- **Per-screen overlays** -- Break / Changeover buttons per screen card
- **Per-screen active toggle** -- start/stop individual screens independently

### Shape Types

| Type | Description |
|------|-------------|
| `process` | Machine/station cell with state color, count, child icons |
| `press` | Stamping press with stroke count, coil level, die change tracking |
| `buffer` | Circular buffer indicator |
| `multibuffer` | Wide rectangular buffer indicator |
| `arrow` | Flow connector between shapes |
| `header` | Top banner with gradient background and logo |
| `image` | External image placement |
| `final` | 4-column KPI display (Plan, Actual, Uptime, Performance) |
| `status` | Full-width overlay banner for messages |

### Child Icons

Process and press shapes can contain child icons positioned relative to the parent:

| Icon | Purpose |
|------|---------|
| `diamond` | Starved indicator |
| `gear` | Setup/changeover active |
| `stopwatch` | Overcycle timer |
| `x` | Blocked / die protect |
| `label` | Text label |
| `wrench` | Maintenance |
| `pm` | Preventive maintenance |
| `preflight` | Tall rectangle (customizable color) |
| `feeder` | Rounded rectangle showing coil level |

### Visual Mappings (Rules Engine)

A declarative rules engine maps PLC bitfield state to visual properties (background color, flashing, glow, child visibility, etc.). Rules are fully customizable per shape type. Bit references support `!` prefix for inversion (e.g. `!computedState.3` matches when the bit is NOT set).

See [docs/visual-mappings.md](docs/visual-mappings.md) for the complete guide.

### PLC State Bitfields

**Process** (26 bits): heartbeat, inAuto, inManual, faulted, inCycle, eStop, clearToEnter, lcBroken, cycleStart, starved, blocked, redRabbit, mhPartPresent, staPartPresent, prodAndon, maintAndon, logisticsAndon, qualityAndon, hrAndon, emergencyAndon, toolingAndon, engineeringAndon, controlsAndon, itAndon, partKicked, toolChangeActive

**Press** (15 bits): heartbeat, inAuto, inContinuous, dieChange, faulted, eStop, topStop, setup1-4, coilEnd, dieProtect, lubeProtect, strokeComplete

### Computed State

The server computes a `computedState` DINT sent alongside the PLC state. These bits are derived from production targets, shift schedules, live counts, and cycle timing:

| Bit | Label | Description |
|-----|-------|-------------|
| 0 | behind | Actual count is below expected count for the current shift hour |
| 1 | overcycle | Current man or machine cycle time exceeds configured target |
| 2 | firstHourMet | First hour production target was met (actual >= planned in DB) |
| 3 | firstHourComplete | First shift hour has elapsed |

Computed state bits can be used in visual mapping rules like any other bit (e.g. `computedState.0`, `!computedState.3`).

### Overcycle Detection

For process shapes with Man Time and Machine Time configured, the server tracks the man/machine cycle:

1. **Man phase** starts on rising edge of `clearToEnter` (state.6)
2. **Machine phase** starts on rising edge of `cycleStart` (state.8)
3. If elapsed time exceeds the target for the active phase, `computedState.1` (overcycle) is set
4. The bit clears on every phase transition
5. Faults (state.3) or E-stops (state.5) reset tracking to idle

Targets are resolved from the active job style's Man Time / Machine Time configuration.

### Count Decoding

Process count tags are 32-bit DINTs: low 16 bits = Part ID, high 16 bits = counter. The `text` mapping rule controls decoding via `{ "source": "count", "decode": "high16" }`.

### Hourly Production Tracking

The server tracks part counts per shape, per shift hour, in a SQLite database. Planned counts are computed from takt time and configured work minutes per hour. The `/counts` page shows a live hourly breakdown with date navigation.

### Reports

The `/reports` page provides 12 report types accessible via `GET /api/reports?report_type=...`:

| Report Type | Description |
|-------------|-------------|
| `downtime` | Fault and e-stop duration per shift hour |
| `operator_entry` | Operator cell entry time |
| `operator_overcycle` | Operator time exceeding man time target |
| `machine_overcycle` | Machine time exceeding machine time target |
| `starved_blocked` | Starved or blocked duration |
| `red_rabbit` | Quality hold duration |
| `tool_change` | Tool/job changeover duration and count |
| `oee` | Overall Equipment Effectiveness (Availability x Performance x Quality) |
| `andon_response` | Duration of each andon call type |
| `production_trend` | Daily part counts over time (chart data) |
| `tool_change_trend` | Daily tool change duration over time |
| `fault_trend` | Daily fault/e-stop duration over time |

### Backup & Restore

The server supports automated backup to S3-compatible storage or a central HTTP server. A 3-slot ring buffer retains the last 3 database snapshots. Configuration history is preserved in S3. The `restore` subcommand restores from backup before server start. See [docs/backup-restore.md](docs/backup-restore.md).

## Architecture

```
Warlink PLC Bridge  ──SSE──>  Andon Server  ──SSE──>  Browser Display
                                   │
                              config.json     (screens, settings, mappings, targets)
                              andon.db        (SQLite: state events, hourly counts)
```

- **Server** (`cmd/andon/`) -- Go HTTP server, SSE hub, Warlink client, event logger
- **Render Engine** (`static/render.js`) -- shared ES module used by both designer and display
- **Templates** (`templates/`) -- dashboard, designer, display, settings, counts, reports, visual-mappings

## Pages

| Path | Auth | Description |
|------|------|-------------|
| `/` | No | Dashboard with screen grid and global controls |
| `/screens/{slug}` | No | Full-screen TV display |
| `/counts` | No | Hourly production counts view |
| `/reports` | No | Shift reports and analytics |
| `/login` | -- | Login page |
| `/designer` | Yes | Layout editor (canvas drag-and-drop) |
| `/configure/{id}` | Yes | Per-cell target configuration |
| `/settings` | Yes | Station, Warlink, shifts, password |
| `/visual-mappings` | Yes | Visual mapping rules editor |

## API

All mutation endpoints require authentication unless noted.

### Screens

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/screens` | List all screens |
| `POST` | `/api/screens` | Create screen |
| `GET` | `/api/screens/{id}` | Get screen |
| `PUT` | `/api/screens/{id}` | Update screen name |
| `DELETE` | `/api/screens/{id}` | Delete screen |
| `GET/PUT` | `/api/screens/{id}/layout` | Screen layout JSON |
| `GET/PUT` | `/api/screens/{id}/config` | Cell target parameters |
| `PUT` | `/api/screens/{id}/overlay` | Per-screen overlay toggle |
| `PUT` | `/api/screens/{id}/active` | Per-screen active toggle |
| `PUT` | `/api/screens/{id}/auto-start` | Auto-start on shift toggle |

### Global & Settings

| Method | Path | Description |
|--------|------|-------------|
| `GET/PUT` | `/api/global` | Global state (blackout, overlay, andon) |
| `GET/PUT` | `/api/settings` | Station settings |
| `PUT` | `/api/auth/password` | Change admin password |
| `GET/PUT` | `/api/visual-mappings` | Visual mapping rules |
| `GET/PUT` | `/api/reporting-units` | Reporting unit list |

### Production Data

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/hourly-counts` | Hourly counts (query: `screen`, `date`, `shape`) |
| `GET` | `/api/reports` | Shift reports (query: `report_type`, `screen`, `date`, `shift`, `shape`, `days`, `style`) |
| `GET` | `/api/shift-summary` | Pre-computed shift summaries (query: `screen`, `from`, `to`, `shift`, `shape`) |
| `POST` | `/api/shift-summary/recompute` | Recompute summaries for a date range |

### Export

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/export/hourly-counts.csv` | Export hourly counts as CSV |
| `GET` | `/api/export/hourly-counts.xlsx` | Export hourly counts as XLSX |
| `GET` | `/api/export/shift-summary.csv` | Export shift summaries as CSV |
| `GET` | `/api/export/shift-summary.xlsx` | Export shift summaries as XLSX |
| `GET` | `/api/export/reports.csv` | Export report data as CSV |
| `GET` | `/api/export/reports.xlsx` | Export report data as XLSX (use `report_type=all` for multi-sheet) |

### Warlink Proxy

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/warlink/test` | Test Warlink connectivity |
| `GET` | `/api/warlink/plcs` | List PLCs |
| `GET` | `/api/warlink/tags/{plc}` | List tags for a PLC |

### Event Log

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/eventlog/status` | Database size and row counts |
| `POST` | `/api/eventlog/prune` | Delete events before timestamp |

### Backup

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/backup/status` | Backup status (last push times, errors, next scheduled) |
| `POST` | `/api/backup/trigger` | Trigger immediate full backup |

### SSE Streams

| Path | Events | Description |
|------|--------|-------------|
| `/events/{slug}` | `state`, `update`, `cell_data`, `screen_overlay`, `screen_active` | Display stream |
| `/events/_dashboard` | `state`, `screen_active_change` | Dashboard stream |

## Documentation

- [PLC State Tags](docs/plc-state-tags.md) -- state DINT bitfield specification for process and press
- [Visual Mappings Guide](docs/visual-mappings.md) -- rules engine reference
- [Computed State & Production Tracking](docs/computed-state.md) -- server-side bit computation
- [Reports](docs/reports.md) -- report types, query parameters, and methodology
- [Export](docs/export.md) -- CSV and XLSX export endpoints
- [Shift Summary](docs/shift-summary.md) -- pre-computed shift metrics and background sweep
- [Backup & Restore](docs/backup-restore.md) -- S3 and central server backup, CLI restore
- [Rendering Notes](docs/rendering.md) -- canvas component rendering details
- [Dev Tools](docs/dev-tools.md) -- PLC simulator and historical data generator for testing

### Integrations

- [Grafana Integration](docs/integration-grafana.md) -- JSON API datasource with Infinity plugin, example dashboards, variables
- [Power BI Integration](docs/integration-powerbi.md) -- Web connector, Power Query, CSV/XLSX import, DAX measures
- [Apache Superset Integration](docs/integration-superset.md) -- CSV upload, direct SQLite, SQL Lab queries, scheduled ingestion

## License

Proprietary. All rights reserved.
