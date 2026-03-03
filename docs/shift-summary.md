# Shift Summary

The shift summary system pre-computes and persists comprehensive per-shape, per-shift, per-style metrics in a SQLite table. This avoids expensive on-the-fly event log walking for historical data.

## Table Schema

The `shift_summary` table has a unique constraint on `(screen_id, shape_id, shift_name, job_style, count_date)`. Rows are upserted (INSERT ... ON CONFLICT DO UPDATE) so recomputation is idempotent.

### Fields

| Field | Type | Description |
|-------|------|-------------|
| screen_id | TEXT | Screen UUID |
| shape_id | TEXT | Shape UUID |
| shape_name | TEXT | Shape display name (denormalized from layout) |
| shape_type | TEXT | `process` or `press` |
| shift_name | TEXT | Shift name |
| job_style | TEXT | PLC style value (empty string for default) |
| style_name | TEXT | Resolved style name |
| count_date | TEXT | Date (YYYY-MM-DD) |
| actual | INT | Total parts produced |
| planned | INT | Total planned parts |
| early | INT | Parts produced before shift start |
| overtime | INT | Parts produced after shift end |
| availability | REAL | OEE availability factor |
| performance | REAL | OEE performance factor |
| quality | REAL | OEE quality factor |
| oee | REAL | Overall Equipment Effectiveness |
| downtime_seconds | REAL | Total downtime (faulted + e-stop) |
| downtime_count | INT | Number of downtime events |
| operator_entry_seconds | REAL | Total operator entry time |
| operator_overcycle_seconds | REAL | Operator overcycle time |
| machine_overcycle_seconds | REAL | Machine overcycle time |
| starved_blocked_seconds | REAL | Starved/blocked duration |
| tool_change_seconds | REAL | Tool change duration |
| tool_change_count | INT | Number of tool changes |
| red_rabbit_seconds | REAL | Quality hold duration |
| scrap_count | INT | Parts scrapped (partKicked rising edges) |
| andon_prod_seconds | REAL | Production andon duration |
| andon_maint_seconds | REAL | Maintenance andon duration |
| andon_logistics_seconds | REAL | Logistics andon duration |
| andon_quality_seconds | REAL | Quality andon duration |
| andon_hr_seconds | REAL | HR andon duration |
| andon_emergency_seconds | REAL | Emergency andon duration |
| andon_tooling_seconds | REAL | Tooling andon duration |
| andon_engineering_seconds | REAL | Engineering andon duration |
| andon_controls_seconds | REAL | Controls andon duration |
| andon_it_seconds | REAL | IT andon duration |
| total_work_seconds | REAL | Total configured work seconds for the shift |
| style_minutes | INT | Minutes this style ran |
| computed_at | TEXT | ISO 8601 timestamp of computation |

## Computation

Each summary row is computed by running ALL event log walkers once per shape:
1. Query state rows for the shape within the shift time range
2. Run downtime, operator entry, operator overcycle, machine overcycle, starved/blocked, tool change, red rabbit, scrap, and all 10 andon walkers
3. Query hourly_counts to aggregate actual/planned/early/overtime by style
4. Compute OEE values from availability, performance, and quality

When a shape ran multiple styles during a shift, one row is produced per style with production counts split by style. Duration metrics (downtime, overcycle, etc.) are shared across style rows since they are not style-specific.

## Automatic Background Sweep

The `summarySweep` goroutine runs every 30 minutes and backfills missing summaries:
- Checks shifts that ended in the last 48 hours
- For each screen and completed shift, checks if a summary row exists
- If missing, computes and inserts the summary
- Logs each backfilled entry

This ensures summaries are computed even if the server was down when a shift ended.

## Manual Recomputation

### API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/shift-summary` | No | Query shift summaries with filters |
| `POST` | `/api/shift-summary/recompute` | Yes | Recompute summaries for a date range |

### Query API

`GET /api/shift-summary?screen=ID&from=YYYY-MM-DD&to=YYYY-MM-DD&shift=NAME&shape=ID`

All parameters except `screen`, `from`, and `to` are optional filters. Returns an array of ShiftSummaryRow objects ordered by date, shape name, shift name, and style.

### Recompute API

`POST /api/shift-summary/recompute` with JSON body:
```json
{
  "screen_id": "uuid",
  "from": "2024-01-01",
  "to": "2024-01-31"
}
```

Iterates each day in the range and each configured shift, computing and upserting summary rows. Returns the number of shift-days processed.

## Relationship to Other Systems

- **Hourly counts**: Summary aggregates hourly_counts for actual/planned/early/overtime
- **Event log**: Summary walks event log state rows for all duration metrics
- **Computed state**: firstHourMet and behind bits are not stored in summaries (they are live-only)
- **Export**: Shift summary data can be exported via `/api/export/shift-summary.csv` and `.xlsx`
