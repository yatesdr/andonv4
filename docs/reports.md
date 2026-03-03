# Reports

Reports are accessed via `GET /api/reports` with query parameters. The `/reports` page provides a UI for viewing them.

## Query Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `screen` | Yes | Screen ID |
| `report_type` | Yes | One of the report types below |
| `date` | No | Date in YYYY-MM-DD format (default: today) |
| `shift` | No | Filter to a specific shift name |
| `shape` | No | Filter to a specific shape ID |
| `days` | No | Number of days for trend reports (default: 30, "all" = 365) |
| `style` | No | Filter to a specific job style (production_trend only) |

## Report Types

### Duration Reports (hourly breakdown)

These reports walk the event log state rows for a shift and compute durations based on bit conditions. They return operations grouped by shape, each with shifts containing hourly buckets.

Response structure:
```json
{
  "report_type": "...",
  "operations": [{
    "shape_id": "uuid",
    "name": "OP-100",
    "shifts": [{
      "name": "Day",
      "time_range": "06:00 - 14:00",
      "hours": [{"shift_hour": 1, "seconds": 120.5, "count": 3}],
      "total_seconds": 450.2,
      "total_count": 8
    }]
  }],
  "hours": [{"shift_hour": 1, "index": 1, "label": "6:00", "start_min": 360}]
}
```

| Type | Description | Measurement |
|------|-------------|-------------|
| `downtime` | Time in faulted or e-stop state | Duration when state.3 (faulted) or state.5 (eStop) is set, excluding when bit is set during non-work break periods |
| `operator_entry` | Time operator is inside the cell | From clearToEnter (state.6) rising edge to lcBroken (state.7) rising edge |
| `operator_overcycle` | Operator time exceeding Man Time target | Same interval as operator_entry minus configured Man Time per cycle |
| `machine_overcycle` | Machine time exceeding Machine Time target | From cycleStart (state.8) to next clearToEnter, minus configured Machine Time |
| `starved_blocked` | Time starved or blocked | Duration when state.9 (starved) or state.10 (blocked) is set, filtered to machine phase only |
| `red_rabbit` | Quality hold duration | Duration when state.11 (redRabbit) is set |
| `tool_change` | Tool/job changeover duration | Duration when state.25 (toolChangeActive) is set, with rising edge count |

### OEE Report

| Type | Description |
|------|-------------|
| `oee` | Overall Equipment Effectiveness per shape per shift |

Response structure:
```json
{
  "report_type": "oee",
  "operations": [{
    "shape_id": "uuid",
    "name": "OP-100",
    "shifts": [{
      "name": "Day",
      "time_range": "06:00 - 14:00",
      "availability": 0.95,
      "performance": 0.88,
      "quality": 0.99,
      "oee": 0.83,
      "downtime_seconds": 1440.0,
      "actual": 450,
      "planned": 500,
      "scrapped": 3
    }]
  }]
}
```

**OEE Formula:**
- **Availability** = (TotalWorkSeconds - DowntimeSeconds) / TotalWorkSeconds
- **Performance** = Actual / Planned (from hourly_counts table)
- **Quality** = (Actual - Scrapped) / Actual (scrapped = partKicked rising edges)
- **OEE** = Availability x Performance x Quality

TotalWorkSeconds is the sum of configured work minutes per hour for the shift, converted to seconds. Defaults to 8 hours (28800s) if no minutes are configured.

### Andon Response Report

| Type | Description |
|------|-------------|
| `andon_response` | Duration of each andon call type per shape per shift |

Response structure:
```json
{
  "report_type": "andon_response",
  "operations": [{
    "shape_id": "uuid",
    "name": "OP-100",
    "shifts": [{
      "name": "Day",
      "time_range": "06:00 - 14:00",
      "andons": [
        {"type": "prod", "label": "Production", "seconds": 120.5},
        {"type": "maint", "label": "Maintenance", "seconds": 45.0}
      ],
      "total_seconds": 165.5
    }]
  }]
}
```

The 10 andon categories tracked: Production, Maintenance, Logistics, Quality, HR, Emergency, Tooling, Engineering, Controls, IT.

### Trend Reports

Trend reports aggregate data across multiple days. They return labels (date strings) and datasets for charting.

Response structure:
```json
{
  "report_type": "production_trend",
  "labels": ["Feb 1", "Feb 2", "Feb 3"],
  "datasets": [
    {"shift": "Day", "style": "Default", "data": [100, 120, 115]},
    {"shift": "Night", "style": "Default", "data": [90, 95, 88]}
  ]
}
```

| Type | Description | Data Source |
|------|-------------|-------------|
| `production_trend` | Daily part counts by shift and style | hourly_counts table |
| `tool_change_trend` | Daily tool change duration by shift | Event log state walking |
| `fault_trend` | Daily fault/e-stop duration by shift | Event log state walking |

### Report Session Context

Duration reports use shift-aware hour bucketing:
- Break periods (from configured Minutes per hour) are excluded from duration accumulation
- Hours are 1-indexed from shift start (hour 1 = first 60 minutes of shift)
- Overnight shifts are handled transparently
