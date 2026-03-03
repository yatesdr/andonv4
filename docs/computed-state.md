# Computed State & Production Tracking

The Andon server computes a `computedState` DINT for each process and press shape, sent alongside PLC state via SSE. These bits provide production intelligence the PLC cannot compute on its own.

## Computed State Bits

| Bit | Mask | Label | Description |
|-----|------|-------|-------------|
| 0 | 0x01 | behind | Actual parts < expected parts for current shift hour |
| 1 | 0x02 | overcycle | Current man or machine phase exceeds time target |
| 2 | 0x04 | firstHourMet | Hour 1 production target was met (actual >= planned in DB) |
| 3 | 0x08 | firstHourComplete | First shift hour has elapsed (shiftHour >= 2) |

## Behind Detection (Bit 0)

Runs every 1 second in the behind ticker. For each shape with a configured takt time:

1. Accumulates `expectedParts` based on elapsed seconds / takt time
2. Compares against `actualParts` (cumulative count deltas since shift start)
3. Sets bit 0 when `actual < floor(expected)`
4. Clears bit 0 when actual catches up

Break allowances are subtracted from expected accumulation when the screen overlay is set to "BREAK" and the configured break minutes for that hour haven't been exhausted.

## Overcycle Detection (Bit 1)

Tracks man/machine cycle timing for process shapes with Man Time and Machine Time configured.

### Cycle Model

```
clearToEnter (state.6) ──> Operator works ──> cycleStart (state.8) ──> Machine works ──> clearToEnter
      rising edge            [MAN TIME]           rising edge          [MACHINE TIME]      rising edge
```

### State Machine

- **idle**: No timing active. Waiting for first clean transition.
- **man**: Timing operator work. Started on rising edge of state.6 (clearToEnter).
- **machine**: Timing machine work. Started on rising edge of state.8 (cycleStart).

### Transitions

| Event | Action |
|-------|--------|
| state.6 rising edge | Enter man phase, clear overcycle, start timer |
| state.8 rising edge | Enter machine phase, clear overcycle, start timer |
| state.3 (faulted) active | Go idle, clear overcycle |
| state.5 (eStop) active | Go idle, clear overcycle |
| clearToEnter drops during man phase | Go idle, clear overcycle (safety re-engaged) |

### Overcycle Check

Every 1 second (piggybacks on the behind ticker):

- If in man phase and elapsed > ManTime target: set bit 1
- If in machine phase and elapsed > MachineTime target: set bit 1
- Targets are resolved from the active job style's per-style ManTime/MachineTime, falling back to the shape's default values

### Design Philosophy

Overcycle detection is purely a live visual indicator. Any abnormal condition (fault, e-stop, safety re-engagement) clears the bit and returns to idle. No persistence or logging -- downstream reporting can extract timing data from the event log database.

## First Hour Met (Bit 2)

Checked once when the shift transitions past hour 1 (shiftHour crosses from 1 to 2):

1. Queries `hourly_counts` table for shift_hour=1 rows
2. Sums actual (delta) and planned values
3. Returns true only when both actual > 0 AND planned > 0 AND actual >= planned
4. If no production data or no planned target exists, returns false

During hour 1, bit 2 is forced ON (optimistic default). Once hour 1 completes, the actual check runs and bit 2 is set or cleared based on real data.

## First Hour Complete (Bit 3)

Set when `computeShiftHour()` returns >= 2. Cleared during hour 1. This bit is useful for visual mapping conditions that should only apply after the first hour has elapsed (e.g. hiding the chip during hour 1 with `!computedState.3`).

## Hourly Count Tracking

### Count Decoding

Process count tags encode Part ID (low 16 bits) and Counter (high 16 bits) in a single DINT. The counter delta is tracked per shape.

### Shift Resolution

- `ResolveShift()` determines the active shift based on current time
- Prefers an upcoming shift within 30-minute tolerance
- Overnight shifts (start > end) are supported
- Count date is the date the shift ends (for overnight shifts)

### Hour Bucketing

Hours are shift-relative, not clock-relative:
- Hour 1 = shiftStart to shiftStart+60 minutes
- `computeShiftHour()` takes current minutes since midnight and returns 1-based hour index

### Planned Computation

After each count delta batch insert, planned values are recomputed:
- Work minutes for the hour (from shift config, accounting for breaks)
- Takt time for the active style
- `planned = workMinutes / (taktTime / 60)`
- When multiple styles run in the same hour, planned is proportioned by style_minutes

### API

`GET /api/hourly-counts?screen=ID&date=YYYY-MM-DD&shape=ID`

Returns operations grouped by shape (layout order), each containing shifts with style sub-rows and aggregated hourly data.
