# Dev Tools: PLC Simulator & Historical Data Generator

The Dev Tools page (`/devtools`, login required) provides two utilities for developing and testing Andon without access to real PLCs: a **live simulator** that generates real-time PLC events through the full server pipeline, and a **historical data generator** that inserts synthetic data into the database for past dates to exercise reports, exports, and integrations.

## Warning: Production Data Risk

Both tools write directly to the production database (`andon.db`). Generated data is **indistinguishable** from real PLC data in reports, exports, and integrations. Always use **Clear Data** to remove synthetic data before connecting real PLCs or deploying to production.

## Prerequisites

Before using either tool, ensure the following are configured:

1. **At least one screen** with process or press shapes in the layout
2. **Shift schedule** defined under Settings > Shifts (with start/end times and work minutes per hour)
3. **Takt times** configured under Targets for each shape (click Targets on a screen card from the dashboard)

Without shifts and takt times, the simulator has no timing baseline and the generator cannot compute planned counts.

## Live Simulator

### Purpose

The live simulator injects PLC events through the full server pipeline: `InjectEvent` -> `decodeAndBroadcast` -> `eventLog.Record` -> count tracking -> behind/overcycle detection -> SSE broadcast. This exercises every production code path except the actual Warlink SSE connection.

Use it to:
- **Test display pages** -- open `/screens/{slug}` and watch shapes animate in real time
- **Verify visual mappings** -- see how mapping rules respond to state transitions, faults, andons
- **Test behind/overcycle detection** -- use time compression = 1 to run at real-time speed
- **Stress-test the system** -- run at high compression (50-100x) to generate rapid events
- **Verify count tracking** -- check that `/counts` page increments correctly during simulation

### Configuration

| Field | Default | Description |
|-------|---------|-------------|
| Screen | -- | The screen to simulate. All process and press shapes in the layout get individual goroutines. |
| Takt Multiplier | 1.0 | Scales each shape's configured takt time. 0.5 = twice as fast, 2.0 = twice as slow. Man time and machine time scale proportionally. |
| Time Compression | 10 | Wall-clock speed factor. 10 = a 30s takt completes in 3s. Use 1 for real-time testing (e.g. behind/overcycle validation). Use 50-100 to rapidly generate count data. |
| Fault Prob | 0.05 | Probability (0-1) per cycle of a fault injection. Faulted cycles set bit 3 for 5-30 seconds, then resume. Generates downtime visible in reports. |
| Andon Prob | 0.03 | Probability (0-1) per cycle of a random andon call. A random andon type (production, maintenance, logistics, quality, HR, emergency, tooling, engineering, controls, IT) is chosen. Active for 10-60 seconds. |
| Style Values | (empty) | Comma-separated PLC style values to rotate through (e.g. `1,2,3`). Must match values configured in the layout's shape config. Leave empty to skip style rotation. |
| Style Interval | 60 | Seconds between style rotations (real-time, not compressed). Set to 0 to disable. |

### Cycle State Machine

Each shape runs this state machine per cycle:

```
IDLE (inAuto, bit 1)
  |  sleep 0.5s
  v
clearToEnter rising (bit 6) -- man phase
  |  sleep manTime / timeCompression
  |--- [fault_prob] --> FAULTED (bit 3), hold 5-30s, clear -> IDLE
  |--- [andon_prob] --> ANDON (random bit 14-23), hold 10-60s, clear, continue
  v
cycleStart rising (bit 8) -- machine phase start
  |  sleep 0.5s
  v
inCycle rising (bit 4), bits 6/8 cleared
  |  sleep machineTime / timeCompression
  v
Count increment (PartID|Counter DINT)
Clear bit 4 -> IDLE
```

Process count encoding: `int32(partID) | (int32(counter) << 16)`

### How to Use

1. Navigate to `/devtools` (must be logged in)
2. Select a screen from the dropdown
3. Adjust parameters (defaults work well for most testing)
4. Click **Start** -- the screen auto-activates and a count session begins
5. Open `/screens/{slug}` in another tab to see live animation
6. Check `/counts` to see counts incrementing
7. Click **Stop** when done -- the screen stays active

The status bar shows event counts per screen and updates every 2 seconds.

## Historical Data Generator

### Purpose

Inserts synthetic data directly into SQLite for past dates, bypassing the live event pipeline. This immediately populates Production Counts, Shift Reports, and Shift Summary so you can test reporting, exports, and integrations without waiting for real production runs.

Use it to:
- **Test report pages** -- all 12 report types populate with realistic data
- **Test CSV/XLSX exports** -- export endpoints return generated data
- **Test date navigation** -- production counts page shows data for any generated date
- **Test Grafana/Power BI integrations** -- API endpoints return generated data
- **Validate aggregation logic** -- use Verify to check that the read pipeline produces correct results

### Configuration

| Field | Default | Description |
|-------|---------|-------------|
| Screen | -- | The screen to generate data for. All process and press shapes in the layout will get data. |
| Date From | 30 days ago | Start date (inclusive). |
| Date To | Today | End date (inclusive). Avoid today if the simulator or real PLCs are running. |
| Count Variance | 0.15 | Random variance as a fraction of planned. 0.15 = actual will be 85-115% of planned. 0 = exact planned counts. Higher values create more realistic chart variation. |
| Fault Prob | 0.10 | Probability per cycle of a fault. When events are enabled, creates fault/clear state transitions visible in the Downtime report. Also reduces that cycle's output. |
| Andon Prob | 0.05 | Probability per cycle of an andon call. When events are enabled, creates andon state transitions visible in the Andon Response report. |
| Generate process events | checked | Creates state-transition rows in `process_events`. **Required** for daily reports (Downtime, OEE, Operator Entry, Machine Overcycle, Andon Response, Starved/Blocked, etc.). Increases generation time and DB size (~4-5 events per cycle). |
| Generate shift summaries | checked | Creates pre-computed rows in `shift_summary`. Required for the Shift Summary report type. Can also be generated via the Recompute button on the Reports page. |

### What Gets Generated

For each (date, shift, shape, hour):

1. **Hourly counts** (`hourly_counts` table): actual count, planned count, work minutes
2. **Process events** (if enabled, `process_events` table): realistic state-transition events per cycle:
   - Normal cycle: clearToEnter -> cycleStart -> inCycle -> idle (4 events)
   - Faulted cycle: fault set -> inCycle resume -> idle (3 events)
   - Andon cycle: andon bit active -> andon cleared, then normal cycle continues (2 extra events)
3. **Shift summaries** (if enabled, `shift_summary` table): actual, planned, availability, performance, OEE, downtime seconds/count

### Which Reports Use What

| Report Type | Requires Events? | Data Source |
|-------------|-----------------|-------------|
| Shift Summary | No | `shift_summary` table |
| Production Trend | No | `hourly_counts` table |
| Downtime | **Yes** | `process_events` state transitions |
| OEE | **Yes** | `process_events` state transitions |
| Operator Entry | **Yes** | `process_events` (clearToEnter timing) |
| Operator Overcycle | **Yes** | `process_events` (man phase timing) |
| Machine Overcycle | **Yes** | `process_events` (machine phase timing) |
| Starved/Blocked | **Yes** | `process_events` (starved/blocked bits) |
| Red Rabbit | **Yes** | `process_events` (redRabbit bit) |
| Tool Change | **Yes** | `process_events` (toolChangeActive bit) |
| Andon Response | **Yes** | `process_events` (andon bits 14-23) |
| Tool Change Trend | No | `shift_summary` table |
| Fault Trend | No | `shift_summary` table |

### How to Use

1. Select a screen and date range
2. Adjust variance and fault/andon probabilities
3. Enable "Generate process events" for full report coverage
4. Click **Generate** -- progress bar tracks completion
5. Navigate to `/counts` and select the generated dates
6. Navigate to `/reports`, select the screen, and change the date to a generated date

### Clearing Data

Click **Clear Data** to delete all generated rows (`hourly_counts`, `process_events`, `shift_summary`) for the selected screen and date range. This is irreversible. Always clear synthetic data before connecting real PLCs.

## Verification

### Purpose

Compares the exact values the generator inserted (ground truth, held in memory) against what the production API returns when querying hourly counts. This validates that the full read pipeline (SQL aggregation, style grouping, shift bucketing) produces correct results.

### Color Coding

| Color | Meaning |
|-------|---------|
| Green | Both actual and planned match |
| Yellow | Planned count mismatch (usually due to style-time proportional recomputation) |
| Red | Actual count mismatch (indicates a bug in aggregation logic) |

### Limitations

- Ground truth is stored in memory from the last Generate run. Restarting the server clears it -- regenerate before verifying.
- Only checks `hourly_counts` aggregation, not `process_events` or `shift_summary`.
- Maximum 200 rows displayed in the table (all rows are checked in the summary).

## API Reference

All endpoints require authentication. Mounted under `/api/devtools/`.

### Simulator

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `POST` | `/api/devtools/sim/start` | `SimConfig` JSON | Start simulation for a screen |
| `POST` | `/api/devtools/sim/stop` | `{"screen_slug": "..."}` | Stop simulation |
| `GET` | `/api/devtools/sim/status` | -- | List all running simulations |

**SimConfig:**
```json
{
  "screen_slug": "my-screen",
  "screen_id": "uuid",
  "takt_multiplier": 1.0,
  "time_compression": 10,
  "fault_prob": 0.05,
  "andon_prob": 0.03,
  "style_values": [1, 2, 3],
  "style_interval": 60
}
```

### Data Generator

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `POST` | `/api/devtools/generate` | `DataGenConfig` JSON | Start generation (async, returns 202) |
| `GET` | `/api/devtools/generate/progress` | -- | Poll progress (0-1) and last result |
| `POST` | `/api/devtools/verify` | `{"screen_id": "..."}` | Compare ground truth vs API results |
| `POST` | `/api/devtools/clear` | `{"screen_id":"...","date_from":"...","date_to":"..."}` | Delete generated data |

**DataGenConfig:**
```json
{
  "screen_id": "uuid",
  "date_from": "2025-01-01",
  "date_to": "2025-01-31",
  "count_variance": 0.15,
  "fault_prob": 0.10,
  "andon_prob": 0.05,
  "gen_events": true,
  "gen_summary": true
}
```

## Architecture Notes

### Injection Point (Simulator)

The simulator uses `WarlinkClient.InjectEvent()` which constructs a `tagTarget` and calls the existing `decodeAndBroadcast()`. This exercises the full production pipeline: event logging, count tracking, behind/overcycle detection, and SSE broadcast. The only thing bypassed is the actual SSE connection to a Warlink PLC bridge.

### Direct SQL (Data Generator)

The data generator inserts directly into SQLite tables because the event pipeline uses `time.Now()` for timestamps, making it impossible to produce historical data through the normal pipeline. Direct SQL is the only option for backdating data.

### Source Files

| File | Description |
|------|-------------|
| `server/simulator.go` | Live simulator engine and per-shape state machine |
| `server/datagen.go` | Historical data generator, verification, clear |
| `server/api_devtools.go` | API endpoint handlers |
| `server/warlink.go` | `InjectEvent()` method |
| `templates/devtools.html` | Web UI |
