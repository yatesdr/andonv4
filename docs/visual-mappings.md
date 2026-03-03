# Visual Mappings -- Rules Engine

The visual mappings system is a declarative rules engine that controls how PLC state is rendered on the andon display. It maps bitfield values, numeric ranges, and field presence to visual properties like background color, flashing, glow effects, and child icon visibility.

## Accessing the Editor

Navigate to **Visual Mappings** in the top navigation bar (admin-only). The editor has two tabs: **Process** and **Press**, each with its own independent set of bit labels and mapping rules.

## Concepts

### Shape Types

Each shape type (`process` and `press`) has its own mapping definition containing:

1. **Bit Labels** -- human-readable names for each bit position across all DINT fields (`state`, `computedState`)
2. **Mapping Rules** -- ordered rule lists for each visual action

### Actions

An action is a visual property that the rules engine controls. Built-in actions include:

| Action | Type | Description |
|--------|------|-------------|
| `backgroundColor` | Color | Shape fill color |
| `flashing` | Boolean | Alternates background with white at 300ms interval |
| `glow` | Color | Colored shadow/halo drawn behind the shape |
| `chip` | Color | Small colored square in the shape's corner |
| `text` | Text/Object | Displayed text (usually the part count) |
| `child.{name}.visible` | Boolean | Show/hide a child icon (e.g. `child.diamond.visible`) |
| `child.{name}.backgroundColor` | Color | Child icon fill color (e.g. `child.feeder.backgroundColor`) |
| `child.{name}.borderColor` | Color | Child icon border color |

You can add custom actions by clicking **+ Add Action** at the bottom of the rules section.

### Rule Evaluation: Last Match Wins

Rules within each action are evaluated **top to bottom**. Every rule whose condition matches is considered, and the **last matching rule's value is used**. This means:

- Put your **default/fallback** rule at the **top**
- Put **more specific overrides below** it
- The most specific matching rule always takes priority

**Example -- process backgroundColor:**

| # | Condition | Value | Purpose |
|---|-----------|-------|---------|
| 1 | default | `#C0C0C0` (gray) | Fallback when no data |
| 2 | bits (any): state.1 | `#00FF00` (green) | Running in auto |
| 3 | bits (any): state.3, state.5 | `#FF0000` (red) | Faulted or E-Stop |
| 4 | bits (any): state.25 | `#8B00FF` (violet) | Tool/job change |

If both `state.1` (inAuto) and `state.3` (faulted) are set, rules 2 and 3 both match, but rule 3 is later so the shape turns **red**. If `state.25` is also set, rule 4 wins and the shape turns **violet**.

## Condition Types

### default

Always matches. Use as the first rule in any action to set a baseline value.

```
Condition: default
Value: #C0C0C0
```

### bits (any)

Matches if **any** of the listed bit references are set (logical OR). Bit references use the format `field.N` where `field` is the DINT field name (`state` or `computedState`) and N is the bit position (0-indexed).

```
Condition: bits (any) -- state.3, state.5
Value: #FF0000
```

This matches if bit 3 OR bit 5 is set in the state DINT. You can mix fields in a single condition:

```
Condition: bits (any) -- state.3, computedState.0
Value: #FF0000
```

### Bit Inversion

Any bit reference can be prefixed with `!` to invert it -- matching when the bit is **NOT** set. This works in both `any` and `all` conditions:

```
Condition: bits (any) -- !computedState.3
Value: #00FF00
```

This matches when `computedState.3` is NOT set (i.e., still in the first hour). Useful for showing a value only during a specific phase.

### bits (all)

Matches only if **all** listed bit references are set (logical AND).

```
Condition: bits (all) -- state.1, state.4
Value: #00FF00
```

This matches only when both bit 1 AND bit 4 are set.

### range

Matches when a numeric field falls within a specified range. Supports optional `min` and `max` bounds (inclusive).

```
Condition: range -- field: coilPct, min: 30.01
Value: #00FF00
```

| min | max | Meaning |
|-----|-----|---------|
| set | set | `min <= value <= max` |
| set | -- | `value >= min` |
| -- | set | `value <= max` |
| -- | -- | field exists (same as has_field) |

### has_field

Matches if the named field exists and is non-null in the live PLC data. Useful for detecting whether a shape is receiving data at all.

```
Condition: has_field -- field: state
Value: #FFFF00
```

## Bit Labels

The **Bit Labels** section at the top of each tab maps bit positions to human-readable names. These labels serve two purposes:

1. **Hints in the rule editor** -- when you reference `state.3` in a bits condition, the editor shows its label (e.g. "faulted") as a hint
2. **Sim panel in designer** -- the designer's simulation panel uses these labels for bit toggle checkboxes

### Editing Labels

- Click any text field to rename a label
- Click **+ Add Bit** to add the next sequential bit position
- Click the **x** button to remove a label

### Default Process Bit Labels (26 bits)

| Bit | Label | Bit | Label |
|-----|-------|-----|-------|
| state.0 | heartbeat | state.13 | staPartPresent |
| state.1 | inAuto | state.14 | prodAndon |
| state.2 | inManual | state.15 | maintAndon |
| state.3 | faulted | state.16 | logisticsAndon |
| state.4 | inCycle | state.17 | qualityAndon |
| state.5 | eStop | state.18 | hrAndon |
| state.6 | clearToEnter | state.19 | emergencyAndon |
| state.7 | lcBroken | state.20 | toolingAndon |
| state.8 | cycleStart | state.21 | engineeringAndon |
| state.9 | starved | state.22 | controlsAndon |
| state.10 | blocked | state.23 | itAndon |
| state.11 | redRabbit | state.24 | partKicked |
| state.12 | mhPartPresent | state.25 | toolChangeActive |

### Default Press Bit Labels (15 bits)

| Bit | Label | Bit | Label |
|-----|-------|-----|-------|
| state.0 | heartbeat | state.8 | setup2 |
| state.1 | inAuto | state.9 | setup3 |
| state.2 | inContinuous | state.10 | setup4 |
| state.3 | dieChange | state.11 | coilEnd |
| state.4 | faulted | state.12 | dieProtect |
| state.5 | eStop | state.13 | lubeProtect |
| state.6 | topStop | state.14 | strokeComplete |
| state.7 | setup1 | | |

### Computed State Bit Labels (shared by both types)

The `computedState` field is a server-computed DINT that provides bits the PLC cannot calculate on its own, such as schedule adherence and cycle time violations.

| Bit | Label | Description |
|-----|-------|-------------|
| computedState.0 | behind | Actual count is below planned count for the current hour |
| computedState.1 | overcycle | Man or machine cycle time exceeds configured target |
| computedState.2 | firstHourMet | First hour production target was met (actual >= planned) |
| computedState.3 | firstHourComplete | First shift hour has elapsed |

These bits are computed by the Go server based on production targets, shift schedules, and live counts, then sent alongside the PLC state DINT. They can be used in mapping rules exactly like PLC state bits:

```
Condition: bits (any) -- computedState.0
Value: #FF0000
```

## Value Types

The value input adapts based on the action:

### Color Actions

Actions named `backgroundColor`, `glow`, `chip`, or ending in `Color`, `.backgroundColor`, or `.borderColor` show a **color picker** alongside a text field. You can:

- Use the color picker to select a color visually
- Type a hex value directly (e.g. `#FF6A00`)
- Type `null` to clear (no color / disabled)
- Type a JSON object for dynamic values (e.g. `{"source":"self","field":"backgroundColor"}`)

**Glow** accepts any hex color. The glow renders as a colored shadow/halo behind the shape.

### Boolean Actions

Actions named `flashing` or ending in `.visible` show a **checkbox**. Checked = `true`, unchecked = `false`.

### Text / Object Actions

All other actions (e.g. `text`) show a **text field**. You can enter:

- A plain string value
- `null`
- A JSON object for computed values, e.g.: `{"source":"count","decode":"high16"}`

#### Count Decoding Options

The `text` action commonly uses object values to decode PLC count DINTs:

| Value | Result |
|-------|--------|
| `{"source":"count","decode":"high16"}` | High 16 bits of count DINT (process part count) |
| `{"source":"count"}` | Raw count value |
| `{"source":"buffer","decode":"high16"}` | High 16 bits of buffer DINT |
| `-` | Literal dash (no data placeholder) |

## Reordering Rules

Use the **up/down arrows** on each rule row to change its position. Since last-match-wins, moving a rule down gives it higher priority.

## Adding and Removing

- **+ Add Rule** -- appends a new default rule to an action
- **+ Add Action** -- prompts for an action name and creates a new action card
- **x** on a rule row -- deletes that rule
- **x** on an action card header -- deletes the entire action and all its rules

## Import / Export

At the bottom of each tab, the **Import / Export** card lets you save and load mappings as JSON files:

- **Export** -- downloads the current tab as `process-bitmap.json` or `press-bitmap.json`
- **Import** -- loads a JSON file and replaces the current tab's mappings (the file must contain `bit_labels` and `mapping` keys)

Imported changes are loaded into the editor but **not saved** until you click **Save**. This lets you review before committing.

Use export/import to:
- Back up mappings before making changes
- Copy mappings between Andon v4 installations
- Share mapping configurations across teams

## Saving and Resetting

The sticky footer bar at the bottom of the page has two actions:

- **Save** -- writes the current mappings (both process and press) to the server via `PUT /api/visual-mappings`
- **Reset to Defaults** -- after confirmation, clears all custom mappings and reverts to the built-in defaults

Saved mappings take effect immediately on all connected displays.

## Default Mapping Reference

### Process -- Background Color Priority

| Priority | Condition | Color | State |
|----------|-----------|-------|-------|
| Lowest | default | Gray `#C0C0C0` | No data / idle |
| | inManual (state.2) | Gray `#C0C0C0` | Manual mode |
| | inAuto (state.1) | Green `#00FF00` | Running |
| | faulted/eStop (state.3/5) | Red `#FF0000` | Fault |
| Highest | toolJobChange (state.25) | Violet `#8B00FF` | Changeover |

### Process -- Glow

| Condition | Color |
|-----------|-------|
| prodAndon, mhPartPresent, logisticsAndon, qualityAndon, engineeringAndon, controlsAndon, itAndon | Cyan `#00D4FF` |
| maintAndon, hrAndon, toolingAndon | Orange `#FF6A00` |

### Press -- Background Color Priority

| Priority | Condition | Color | State |
|----------|-----------|-------|-------|
| Lowest | default | Gray `#C0C0C0` | No data |
| | has_field: state | Yellow `#FFFF00` (flash) | Receiving data, manual |
| | inContinuous (state.2) | Green `#00FF00` | Continuous mode |
| | inAuto (state.1) | Green `#00FF00` (flash) | Auto mode |
| | faulted/eStop/topStop (state.4/5/6) | Red `#FF0000` | Fault |
| Highest | dieChange (state.3) | Violet `#8B00FF` | Die change |

### Press -- Feeder (Coil Level)

| Condition | Color | Meaning |
|-----------|-------|---------|
| coilPct > 30% | Green `#00FF00` | Adequate coil |
| coilPct 5-30% | Orange `#FFA500` | Low coil warning |
| coilPct < 5% | Red `#FF0000` | Critical coil level |
| coilEnd (state.11) | Red `#FF0000` | Coil end detected |
