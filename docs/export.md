# Data Export

Andon provides CSV and XLSX export endpoints for hourly counts, shift summaries, and reports.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/export/hourly-counts.csv` | Export hourly counts as CSV |
| `GET` | `/api/export/hourly-counts.xlsx` | Export hourly counts as XLSX |
| `GET` | `/api/export/shift-summary.csv` | Export shift summaries as CSV |
| `GET` | `/api/export/shift-summary.xlsx` | Export shift summaries as XLSX |
| `GET` | `/api/export/reports.csv` | Export report data as CSV |
| `GET` | `/api/export/reports.xlsx` | Export report data as XLSX |

## Common Query Parameters

| Parameter | Description |
|-----------|-------------|
| `screen` | Screen ID |
| `from` | Start date, YYYY-MM-DD (default: today) |
| `to` | End date, YYYY-MM-DD (default: today) |
| `shape` | Filter to specific shape ID |

## Hourly Counts Export

Query parameters: `screen`, `from`, `to`, `shape`

Columns:
| Column | Description |
|--------|-------------|
| count_date | Date (YYYY-MM-DD) |
| screen_id | Screen UUID |
| shape_id | Shape UUID |
| shape_name | Shape display name from layout |
| shape_type | Shape type (process or press) |
| shift_name | Shift name |
| job_style | PLC style value |
| style_name | Resolved style name from layout config |
| shift_hour | 1-based hour index within the shift |
| actual | Part count delta for this hour |
| planned | Computed planned count for this hour |
| style_minutes | Minutes this style ran during the hour |
| is_early | 1 if this hour is before shift start |
| is_overtime | 1 if this hour is after shift end |

## Shift Summary Export

Query parameters: `screen`, `from`, `to`, `shift`, `shape`

Additional parameter: `shift` — filter to a specific shift name.

Columns include all hourly count fields plus OEE metrics, duration breakdowns, and andon times:
| Column Group | Fields |
|-------------|--------|
| Identity | count_date, screen_id, shape_id, shape_name, shape_type, shift_name, job_style, style_name |
| Counts | actual, planned, early, overtime |
| OEE | availability, performance, quality, oee |
| Downtime | downtime_seconds, downtime_count |
| Cycle Times | operator_entry_seconds, operator_overcycle_seconds, machine_overcycle_seconds |
| Material Flow | starved_blocked_seconds |
| Tool Change | tool_change_seconds, tool_change_count |
| Quality | red_rabbit_seconds, scrap_count |
| Andon (10 types) | andon_prod_seconds, andon_maint_seconds, andon_logistics_seconds, andon_quality_seconds, andon_hr_seconds, andon_emergency_seconds, andon_tooling_seconds, andon_engineering_seconds, andon_controls_seconds, andon_it_seconds |
| Meta | total_work_seconds, style_minutes, computed_at |

## Reports Export

Query parameters: `screen`, `from`, `to`, `report_type`, `shift`, `shape`

The `report_type` parameter determines the exported data format:

### Duration reports (downtime, operator_entry, operator_overcycle, machine_overcycle, starved_blocked, red_rabbit, tool_change)

A `count_date` column is prepended. Columns: count_date, shape_id, shape_name, shift_name, shift_hour, seconds, count, total_seconds, total_count

### OEE report
Columns: count_date, shape_id, shape_name, shift_name, availability, performance, quality, oee, downtime_seconds, actual, planned, scrapped

### Andon response report
Columns: count_date, shape_id, shape_name, shift_name, andon_type, andon_label, seconds

### Production trend report
Columns: date, shift, style, value

### Multi-Sheet XLSX

When `report_type=all`, the XLSX export generates a multi-sheet workbook with one sheet per report type: downtime, operator_entry, operator_overcycle, machine_overcycle, starved_blocked, red_rabbit, tool_change, oee, andon_response. Each sheet uses the column format described above.

## Notes

- Numeric columns in XLSX are stored as numbers for proper Excel handling
- Date range exports iterate each day independently and concatenate results
- The counts page has built-in CSV and XLSX export buttons that use the hourly counts endpoints
