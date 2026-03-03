# Apache Superset Integration

Connect Apache Superset to Andon's data using CSV export endpoints or by querying the SQLite database directly. Build interactive dashboards for OEE, downtime analysis, production trends, and andon response tracking.

## Quick Start

1. Download a CSV from Andon:
   ```
   https://YOUR_ANDON_IP:8090/api/export/shift-summary.csv?screen=SCREEN_ID&from=2024-01-01&to=2024-01-31
   ```
2. In Superset, go to **Data > Upload a CSV**
3. Upload the file, name the table `shift_summary`, click **Save**
4. Go to **SQL Lab**, run:
   ```sql
   SELECT shape_name, shift_name, count_date,
          actual, planned, oee, downtime_seconds
   FROM shift_summary
   ORDER BY count_date, shape_name
   ```
5. Click **Create Chart** to turn the result into a visualization

You now have Andon production data in Superset.

---

## Prerequisites

| Component | Notes |
|-----------|-------|
| Apache Superset | 2.0+ (Docker, pip, or Kubernetes install) |
| Network access | Superset host must reach the Andon server on port 8090 (for CSV download) |
| Andon Server | v4, running with HTTPS |

## Connection Methods

| Method | Best For | Auto-Refresh |
|--------|----------|-------------|
| **CSV upload** | Quick exploration, one-time analysis | Manual re-upload |
| **Direct SQLite** | Full SQL access to raw event data and summaries | On-demand (read-only) |
| **Scheduled CSV ingestion** | Automated daily dashboard refresh | Via script/cron |

### Method 1: CSV Upload (Simplest)

Best for getting started quickly or one-time analysis.

#### Step-by-step

1. Download the CSV from Andon (via browser or curl):
   ```bash
   curl -k "https://YOUR_ANDON_IP:8090/api/export/shift-summary.csv?screen=SCREEN_ID&from=2024-01-01&to=2024-01-31" -o shift_summary.csv
   ```
2. In Superset: **Data > Upload a CSV**
3. Configure the upload:
   - **Table Name**: `shift_summary`
   - **Database**: Select your Superset metadata database (or a dedicated analytics database)
   - **Delimiter**: Comma (default)
   - **Header Row**: 0 (first row is headers)
   - **Columns to parse as dates**: `count_date`
   - **Decimal Character**: `.`
4. Click **Save**
5. Navigate to **SQL Lab** and query the table, or go to **Charts > + Chart** and select the `shift_summary` dataset

Available CSV endpoints:
| Endpoint | Description |
|----------|-------------|
| `/api/export/hourly-counts.csv?screen=ID&from=DATE&to=DATE` | Per-hour production counts |
| `/api/export/shift-summary.csv?screen=ID&from=DATE&to=DATE` | Daily OEE and all duration metrics |
| `/api/export/reports.csv?screen=ID&from=DATE&to=DATE&report_type=TYPE` | Specific report data |

### Method 2: Direct SQLite Connection

If you can access the Andon SQLite database file, Superset can query it directly. This gives full SQL access to all tables.

> **Important**: Connect read-only so Superset never modifies the production database. The database file is located at `~/.andon/andon.db` on the Andon server (default path).

#### Step-by-step

1. Copy or mount the database file to a location the Superset host can read. Options:
   - **NFS/SMB mount** from the Andon server
   - **Scheduled rsync** copying the file periodically
   - **Shared volume** if both run on the same host or in Docker
2. In Superset: **Data > Databases > + Database**
3. Select **SQLite** as the database type
4. Set the SQLAlchemy URI:
   ```
   sqlite:////path/to/andon.db?mode=ro
   ```
   (Four slashes for absolute path; `?mode=ro` for read-only)
5. Under **Advanced > Security**, check **Allow this database to be queried in SQL Lab**
6. Click **Connect**

#### Key Tables

| Table | Description |
|-------|-------------|
| `shift_summary` | Pre-computed daily metrics per shape/shift/style (best for dashboards) |
| `hourly_counts` | Raw hourly production deltas per shape/shift/hour |
| `events` | Raw PLC state events (large, used for detailed analysis) |

#### Useful Queries

**OEE by operation and date:**
```sql
SELECT count_date, shape_name, shift_name,
       ROUND(oee * 100, 1) AS oee_pct,
       actual, planned
FROM shift_summary
WHERE screen_id = 'SCREEN_ID'
  AND count_date BETWEEN '2024-01-01' AND '2024-01-31'
ORDER BY count_date, shape_name
```

**Downtime breakdown by operation:**
```sql
SELECT shape_name, shift_name,
       ROUND(SUM(downtime_seconds) / 60, 1) AS downtime_min,
       ROUND(SUM(starved_blocked_seconds) / 60, 1) AS starved_blocked_min,
       ROUND(SUM(tool_change_seconds) / 60, 1) AS tool_change_min,
       ROUND(SUM(operator_overcycle_seconds) / 60, 1) AS op_overcycle_min,
       ROUND(SUM(machine_overcycle_seconds) / 60, 1) AS mch_overcycle_min
FROM shift_summary
WHERE screen_id = 'SCREEN_ID'
  AND count_date BETWEEN '2024-01-01' AND '2024-01-31'
GROUP BY shape_name, shift_name
ORDER BY downtime_min DESC
```

**Total andon time by category:**
```sql
SELECT
    ROUND(SUM(andon_prod_seconds) / 60, 1) AS production_min,
    ROUND(SUM(andon_maint_seconds) / 60, 1) AS maintenance_min,
    ROUND(SUM(andon_logistics_seconds) / 60, 1) AS logistics_min,
    ROUND(SUM(andon_quality_seconds) / 60, 1) AS quality_min,
    ROUND(SUM(andon_hr_seconds) / 60, 1) AS hr_min,
    ROUND(SUM(andon_emergency_seconds) / 60, 1) AS emergency_min,
    ROUND(SUM(andon_tooling_seconds) / 60, 1) AS tooling_min,
    ROUND(SUM(andon_engineering_seconds) / 60, 1) AS engineering_min,
    ROUND(SUM(andon_controls_seconds) / 60, 1) AS controls_min,
    ROUND(SUM(andon_it_seconds) / 60, 1) AS it_min
FROM shift_summary
WHERE screen_id = 'SCREEN_ID'
  AND count_date BETWEEN '2024-01-01' AND '2024-01-31'
```

**Hourly production detail:**
```sql
SELECT hc.count_date, hc.shape_name, hc.shift_name,
       hc.shift_hour, hc.actual, hc.planned,
       hc.style_name
FROM hourly_counts hc
WHERE hc.screen_id = 'SCREEN_ID'
  AND hc.count_date = '2024-01-15'
ORDER BY hc.shape_name, hc.shift_hour
```

### Method 3: Scheduled CSV Ingestion

For automated daily refresh without direct database access, use a cron job to download CSVs and load them into Superset's database.

#### Example script (`andon_import.sh`)

```bash
#!/bin/bash
ANDON="https://YOUR_ANDON_IP:8090"
SCREEN="YOUR_SCREEN_ID"
TODAY=$(date +%Y-%m-%d)
DB="/path/to/superset_analytics.db"

# Download today's shift summary
curl -sk "$ANDON/api/export/shift-summary.csv?screen=$SCREEN&from=$TODAY&to=$TODAY" \
  -o /tmp/shift_summary_today.csv

# Load into SQLite (used by Superset)
sqlite3 "$DB" <<SQL
.mode csv
.import --skip 1 /tmp/shift_summary_today.csv shift_summary_staging

INSERT OR REPLACE INTO shift_summary
SELECT * FROM shift_summary_staging;

DROP TABLE IF EXISTS shift_summary_staging;
SQL

echo "Imported shift summary for $TODAY"
```

Schedule with cron to run after each shift ends:
```
0 15 * * * /opt/scripts/andon_import.sh   # After day shift
0 23 * * * /opt/scripts/andon_import.sh   # After afternoon shift
0 7  * * * /opt/scripts/andon_import.sh   # After night shift
```

## Finding Your Screen ID

```bash
curl -sk https://YOUR_ANDON_IP:8090/api/screens | python3 -m json.tool
```

Response:
```json
[
  { "id": "a1b2c3d4-...", "name": "Assembly Line 1", "slug": "assembly-line-1" }
]
```

Use the `id` value as the `screen` parameter or in SQL `WHERE screen_id = '...'` clauses.

## Example Charts in Superset

### 1. OEE Trend (Time Series Line Chart)

- **Dataset**: `shift_summary`
- **Chart type**: Time-series Line Chart
- **Time column**: `count_date`
- **Metric**: `AVG(oee)` (displayed as percentage using number formatting: `,.1%`)
- **Dimensions**: `shape_name`
- **Time range**: Last 30 days
- **Row limit**: 10000

### 2. Actual vs. Planned (Mixed Chart)

- **Dataset**: `shift_summary`
- **Chart type**: Mixed Chart (bars + line)
- **X-axis**: `shape_name`
- **Metrics**:
  - Bar: `SUM(actual)`, `SUM(planned)`
  - Line: `AVG(oee)`
- **Filter**: Single `count_date`, single `shift_name`

### 3. Downtime Pareto (Bar Chart)

- **Dataset**: `shift_summary`
- **Chart type**: Bar Chart
- **X-axis**: `shape_name`
- **Metric**: `SUM(downtime_seconds) / 60` (alias: Downtime Minutes)
- **Sort**: Descending
- **Filter**: Date range, shift

### 4. Loss Category Pie Chart

Use SQL Lab to create a virtual dataset:

```sql
SELECT 'Downtime' AS category, SUM(downtime_seconds)/60 AS minutes FROM shift_summary WHERE count_date BETWEEN '2024-01-01' AND '2024-01-31'
UNION ALL
SELECT 'Starved/Blocked', SUM(starved_blocked_seconds)/60 FROM shift_summary WHERE count_date BETWEEN '2024-01-01' AND '2024-01-31'
UNION ALL
SELECT 'Tool Change', SUM(tool_change_seconds)/60 FROM shift_summary WHERE count_date BETWEEN '2024-01-01' AND '2024-01-31'
UNION ALL
SELECT 'Op. Overcycle', SUM(operator_overcycle_seconds)/60 FROM shift_summary WHERE count_date BETWEEN '2024-01-01' AND '2024-01-31'
UNION ALL
SELECT 'Mch. Overcycle', SUM(machine_overcycle_seconds)/60 FROM shift_summary WHERE count_date BETWEEN '2024-01-01' AND '2024-01-31'
UNION ALL
SELECT 'Red Rabbit', SUM(red_rabbit_seconds)/60 FROM shift_summary WHERE count_date BETWEEN '2024-01-01' AND '2024-01-31'
```

Save as a virtual dataset, then create a **Pie Chart** with `category` as the dimension and `minutes` as the metric.

### 5. Andon Response Heatmap

```sql
SELECT shape_name,
       'Production' AS andon_type, andon_prod_seconds / 60 AS minutes FROM shift_summary WHERE count_date = '2024-01-15'
UNION ALL
SELECT shape_name, 'Maintenance', andon_maint_seconds / 60 FROM shift_summary WHERE count_date = '2024-01-15'
UNION ALL
SELECT shape_name, 'Logistics', andon_logistics_seconds / 60 FROM shift_summary WHERE count_date = '2024-01-15'
UNION ALL
SELECT shape_name, 'Quality', andon_quality_seconds / 60 FROM shift_summary WHERE count_date = '2024-01-15'
UNION ALL
SELECT shape_name, 'Tooling', andon_tooling_seconds / 60 FROM shift_summary WHERE count_date = '2024-01-15'
UNION ALL
SELECT shape_name, 'Engineering', andon_engineering_seconds / 60 FROM shift_summary WHERE count_date = '2024-01-15'
```

Save as a virtual dataset, then create a **Heatmap** with `shape_name` on the Y-axis, `andon_type` on the X-axis, and `minutes` as the metric.

## Superset Dashboard Assembly

Once you have several charts, combine them into a dashboard:

1. Go to **Dashboards > + Dashboard**
2. Enter the dashboard name (e.g. "Assembly Line 1 - Production Overview")
3. Click **Edit Dashboard** and drag your charts from the right panel
4. Use **Row** and **Column** layout components to organize:
   - Top row: KPI cards (OEE %, Plan Attainment %, Total Downtime)
   - Middle: Actual vs. Planned bar chart + OEE trend line
   - Bottom: Downtime Pareto + Andon Response breakdown
5. Add **Filter Box** or **Native Filters** for:
   - `count_date` (date range)
   - `shape_name` (operation selector)
   - `shift_name` (shift selector)
6. Click **Save**

## Tips

- **Start with CSV upload**: Get familiar with the data shape before setting up direct database connections or automation.
- **Virtual datasets**: Use SQL Lab to create pre-aggregated or unpivoted views, then save them as virtual datasets for charting. This keeps your charts simple.
- **Shift summary is the workhorse**: It contains 40+ pre-computed fields covering OEE, counts, downtime, overcycle, andon, and tool change metrics. Most dashboards only need this one table.
- **Read-only access**: Always connect to Andon's SQLite database in read-only mode (`?mode=ro`). Never write to the production database from Superset.
- **Jinja templates**: Superset supports Jinja in SQL Lab. Use `{{ from_dttm }}` and `{{ to_dttm }}` for dynamic date ranges in virtual datasets.
- **Multiple screens**: Add `screen_id` as a native filter to let users switch between production lines.
- **Row-level security**: Superset supports RLS rules to restrict which shapes or screens a user can see -- useful for giving each area their own filtered view.
