# PLC State Tag Bitfield Specification

This document defines the expected bit layout for the state DINT tags that the PLC provides to Andon v4 via Warlink. The state tag is a 32-bit integer (DINT) where each bit position carries a specific meaning. Process and press shapes use different bit layouts.

## Tag Roles

Each shape can have multiple PLC tags assigned, identified by role:

| Role | Tag Config Key | Description |
|------|---------------|-------------|
| state | `state_tag` | Machine state bitfield (this document) |
| count | `count_tag` | Part count DINT |
| buffer | `buffer_tag` | Buffer level DINT |
| style | `style_tag` | Active job style number |
| coil | `coil_tag` | Coil percentage (press only) |
| job_count | `job_count_tag` | Job count (press only) |
| spm | `spm_tag` | Strokes per minute (press only) |
| cat1-5 | `cat1_tag`..`cat5_tag` | Category ID fields |

---

## Process State Tag (26 bits)

The process state DINT encodes machine mode, safety status, cycle state, material flow indicators, and andon call buttons. The PLC should set/clear these bits in real time.

### Bit Layout

| Bit | Mask | Label | Description |
|-----|------|-------|-------------|
| 0 | 0x00000001 | heartbeat | Toggling bit indicating PLC communication is alive |
| 1 | 0x00000002 | inAuto | Machine is in automatic mode |
| 2 | 0x00000004 | inManual | Machine is in manual mode |
| 3 | 0x00000008 | faulted | Machine has an active fault |
| 4 | 0x00000010 | inCycle | Machine is actively running a cycle |
| 5 | 0x00000020 | eStop | Emergency stop is active |
| 6 | 0x00000040 | clearToEnter | Safety systems permit operator entry (OK to Enter) |
| 7 | 0x00000080 | lcBroken | Light curtain is broken (operator inside guarding) |
| 8 | 0x00000100 | cycleStart | Cycle start activated (2-second latch from PLC) |
| 9 | 0x00000200 | starved | Station is starved (waiting for parts from upstream) |
| 10 | 0x00000400 | blocked | Station is blocked (downstream full) |
| 11 | 0x00000800 | redRabbit | Red rabbit / quality hold condition active |
| 12 | 0x00001000 | mhPartPresent | Material handler part present |
| 13 | 0x00002000 | staPartPresent | Station part present |
| 14 | 0x00004000 | prodAndon | Production andon call active |
| 15 | 0x00008000 | maintAndon | Maintenance andon call active |
| 16 | 0x00010000 | logisticsAndon | Logistics/materials andon call active |
| 17 | 0x00020000 | qualityAndon | Quality andon call active |
| 18 | 0x00040000 | hrAndon | Human resources andon call active |
| 19 | 0x00080000 | emergencyAndon | Emergency andon call active |
| 20 | 0x00100000 | toolingAndon | Tooling andon call active |
| 21 | 0x00200000 | engineeringAndon | Engineering andon call active |
| 22 | 0x00400000 | controlsAndon | Controls andon call active |
| 23 | 0x00800000 | itAndon | IT andon call active |
| 24 | 0x01000000 | partKicked | Part was kicked/rejected |
| 25 | 0x02000000 | toolChangeActive | Tool or job changeover in progress |

Bits 26-31 are reserved.

### Cycle Timing Sequence

The overcycle detection system tracks operator and machine time using these bits:

```
clearToEnter (bit 6) ↑  →  Operator works  →  cycleStart (bit 8) ↑  →  Machine works  →  clearToEnter (bit 6) ↑
                             [MAN TIME]                                  [MACHINE TIME]
```

1. **clearToEnter** goes HIGH when the safety system permits the operator to enter the cell
2. The operator performs their work (load parts, inspect, etc.)
3. The operator exits and activates **cycleStart** (2-second latch from PLC)
4. The machine runs its cycle; **clearToEnter** goes LOW while the machine is running
5. When the machine completes, **clearToEnter** goes HIGH again, starting the next cycle

The server measures man time (clearToEnter rising edge → cycleStart rising edge) and machine time (cycleStart rising edge → clearToEnter rising edge) against configured targets.

### Heartbeat (Bit 0)

The PLC should toggle bit 0 at a regular interval (typically every 500ms-1s). Andon uses this to detect communication loss. The heartbeat bit is masked out when comparing state changes for event logging (state changes with only bit 0 different are not recorded).

### Andon Calls (Bits 14-23)

Each andon bit represents a different department call button. When an operator presses an andon call button on the station, the corresponding bit goes HIGH. The default visual mappings render:

- **Cyan glow**: prodAndon, mhPartPresent, logisticsAndon, qualityAndon, engineeringAndon, controlsAndon, itAndon
- **Orange glow**: maintAndon, hrAndon, toolingAndon
- **Red glow + flash**: emergencyAndon

A status message banner displays the department name when an andon is active.

### Default Visual Behavior

| State | Background | Border | Flash | Glow |
|-------|-----------|--------|-------|------|
| No data | Gray #C0C0C0 | Black | No | None |
| inManual | Gray #C0C0C0 | Black | No | None |
| inAuto | Green #00FF00 | Black | No | None |
| inAuto + overcycle | Green #00FF00 | Black | Yes | None |
| inAuto + overcycle + inCycle | Orange #FFA500 | Black | Yes | None |
| Faulted or E-Stop | Red #FF0000 | Black | No | None |
| Tool change active | Violet #8B00FF | Black | No | None |
| Behind schedule | (unchanged) | Red | (unchanged) | (unchanged) |

---

## Press State Tag (15 bits)

The press state DINT encodes press mode, fault status, setup states, and material conditions.

### Bit Layout

| Bit | Mask | Label | Description |
|-----|------|-------|-------------|
| 0 | 0x0001 | heartbeat | Toggling bit indicating PLC communication is alive |
| 1 | 0x0002 | inAuto | Press is in automatic mode |
| 2 | 0x0004 | inContinuous | Press is in continuous stroke mode |
| 3 | 0x0008 | dieChange | Die change is in progress |
| 4 | 0x0010 | faulted | Press has an active fault |
| 5 | 0x0020 | eStop | Emergency stop is active |
| 6 | 0x0040 | topStop | Press is at top stop position |
| 7 | 0x0080 | setup1 | Setup condition 1 active |
| 8 | 0x0100 | setup2 | Setup condition 2 active |
| 9 | 0x0200 | setup3 | Setup condition 3 active |
| 10 | 0x0400 | setup4 | Setup condition 4 active |
| 11 | 0x0800 | coilEnd | Coil end detected (material exhausted) |
| 12 | 0x1000 | dieProtect | Die protection fault active |
| 13 | 0x2000 | lubeProtect | Lubrication protection fault active |
| 14 | 0x4000 | strokeComplete | Stroke completed (typically a short pulse) |

Bits 15-31 are reserved.

### Default Visual Behavior

| State | Background | Flash | Glow |
|-------|-----------|-------|------|
| No data | Gray #C0C0C0 | No | None |
| Receiving data (manual) | Yellow #FFFF00 | Yes | None |
| inContinuous | Green #00FF00 | No | None |
| inAuto | Green #00FF00 | Yes | None |
| Faulted / E-Stop / Top Stop | Red #FF0000 | No | None |
| Die change | Violet #8B00FF | No | None |

### Setup Bits (7-10)

Setup bits indicate various setup or adjustment conditions on the press. When any setup bit is active, the gear child icon is shown. These are typically used for die setup, transfer setup, or other maintenance preparations.

### Coil Monitoring

The press coil level is tracked via a separate `coil_tag` (not part of the state DINT). The feeder child icon changes color based on the coil percentage:

| Coil % | Feeder Color |
|--------|-------------|
| > 30% | Green #00FF00 |
| 5-30% | Orange #FFA500 |
| < 5% | Red #FF0000 |
| coilEnd (bit 11) | Red #FF0000 |

---

## Count Tag Encoding

### Process Count

The process count DINT packs two 16-bit values:

```
[  High 16 bits: Counter  |  Low 16 bits: Part ID  ]
     bits 31-16                bits 15-0
```

- **Counter** (high 16): The running part count displayed on the andon
- **Part ID** (low 16): Identifies the part style currently being produced; used for style-based production tracking

The server decodes this via `DecodeProcessCount()` and tracks count deltas by comparing consecutive counter values.

### Press Count

Press count tags use the raw 32-bit value directly as the stroke count. No high/low split.

### Buffer Count

Buffer count tags follow the same encoding as process counts. The high 16 bits are the buffer level counter.

---

## Customizing Bit Labels

Bit labels and their visual mappings are fully customizable through the Visual Mappings editor (`/visual-mappings`). The defaults documented here represent the standard industrial layout, but any bit can be remapped to different visual behaviors without changing the PLC program. See [visual-mappings.md](visual-mappings.md) for the rules engine reference.
