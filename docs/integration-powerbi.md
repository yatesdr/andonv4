# Power BI Integration

Connect Power BI to Andon's JSON API or CSV/XLSX exports to build interactive production reports, OEE dashboards, and trend analysis workbooks.

## Quick Start

1. Open Power BI Desktop
2. Click **Get Data > Web**
3. Enter the Shift Summary URL for your screen:
   ```
   https://YOUR_ANDON_IP:8090/api/shift-summary?screen=YOUR_SCREEN_ID&from=2024-01-01&to=2024-01-31
   ```
4. Power BI will show a table preview -- click **Transform Data** to open Power Query Editor
5. The JSON array is automatically expanded into a table. Click **Close & Apply**.
6. Build visuals using columns like `shape_name`, `count_date`, `oee`, `actual`, `planned`

You now have Andon production data in Power BI.

---

## Prerequisites

| Component | Notes |
|-----------|-------|
| Power BI Desktop | Free download from Microsoft |
| Network access | Power BI machine must reach the Andon server on port 8090 (HTTPS) |
| Andon Server | v4, running with HTTPS |

> **Self-signed certificates**: Power BI may warn about the Andon server's self-signed TLS certificate. When prompted during the Web connector, select "Connect anyway" or install the Andon certificate in your Windows certificate store under Trusted Root Certification Authorities.

## Connection Methods

Power BI can connect to Andon data in three ways:

| Method | Best For | Auto-Refresh |
|--------|----------|-------------|
| **Web connector (JSON API)** | Live dashboards, scheduled refresh | Yes (with Power BI Gateway) |
| **Web connector (CSV export)** | Simple flat tables, no JSON parsing | Yes (with Power BI Gateway) |
| **File import (XLSX/CSV)** | One-time analysis, offline use | Manual re-import |

### Method 1: JSON API via Web Connector (Recommended)

This method pulls live data from Andon's API every time you refresh.

#### Step-by-step

1. **Get Data > Web > Advanced**
2. Enter the URL:
   ```
   https://YOUR_ANDON_IP:8090/api/shift-summary?screen=SCREEN_ID&from=2024-01-01&to=2024-01-31
   ```
3. Click **OK**. If prompted about the certificate, click **Connect**.
4. Set credentials to **Anonymous** (Andon read endpoints are public) and click **Connect**.
5. Power BI opens the Power Query Editor with the JSON array loaded as a list.
6. Click **To Table** (top-left), then expand columns using the expand icon in the column header.
7. Set data types:
   - `count_date` → Date
   - `actual`, `planned`, `early`, `overtime` → Whole Number
   - `oee`, `availability`, `performance`, `quality` → Decimal Number
   - All `*_seconds` fields → Decimal Number
8. Rename the query to something meaningful (e.g. "ShiftSummary").
9. Click **Close & Apply**.

#### Parameterizing Dates

To make the date range dynamic:

1. In Power Query Editor, go to **Manage Parameters > New Parameter**
2. Create two parameters:
   - **StartDate**: Type = Date, Default = 2024-01-01
   - **EndDate**: Type = Date, Default = today's date
3. Edit the "ShiftSummary" query in the Advanced Editor:

```powerquery
let
    StartDate = Date.ToText(StartDate, "yyyy-MM-dd"),
    EndDate = Date.ToText(EndDate, "yyyy-MM-dd"),
    Url = "https://YOUR_ANDON_IP:8090/api/shift-summary?screen=SCREEN_ID&from=" & StartDate & "&to=" & EndDate,
    Source = Json.Document(Web.Contents(Url)),
    AsTable = Table.FromList(Source, Splitter.SplitByNothing(), null, null, ExtraValues.Error),
    Expanded = Table.ExpandRecordColumn(AsTable, "Column1", {
        "shape_name", "shape_type", "shift_name", "style_name", "count_date",
        "actual", "planned", "early", "overtime",
        "availability", "performance", "quality", "oee",
        "downtime_seconds", "downtime_count",
        "operator_entry_seconds", "operator_overcycle_seconds", "machine_overcycle_seconds",
        "starved_blocked_seconds", "tool_change_seconds", "tool_change_count",
        "red_rabbit_seconds", "scrap_count",
        "andon_prod_seconds", "andon_maint_seconds", "andon_logistics_seconds",
        "andon_quality_seconds", "andon_hr_seconds", "andon_emergency_seconds",
        "andon_tooling_seconds", "andon_engineering_seconds",
        "andon_controls_seconds", "andon_it_seconds",
        "total_work_seconds", "style_minutes"
    })
in
    Expanded
```

### Method 2: CSV Export via Web Connector

If you prefer flat CSV data without JSON parsing:

1. **Get Data > Web**
2. Enter the CSV export URL:
   ```
   https://YOUR_ANDON_IP:8090/api/export/shift-summary.csv?screen=SCREEN_ID&from=2024-01-01&to=2024-01-31
   ```
3. Power BI auto-detects the CSV format and shows a table preview.
4. Click **Transform Data** to adjust column types, then **Close & Apply**.

Available CSV endpoints:
| Endpoint | Description |
|----------|-------------|
| `/api/export/hourly-counts.csv` | Hourly production counts per shape/shift/hour |
| `/api/export/shift-summary.csv` | Daily summary with OEE and all duration metrics |
| `/api/export/reports.csv` | Report data (add `&report_type=downtime`, etc.) |

### Method 3: XLSX File Import

For one-time analysis or when the Power BI machine cannot reach the Andon server:

1. Download the XLSX from Andon:
   ```
   https://YOUR_ANDON_IP:8090/api/export/shift-summary.xlsx?screen=SCREEN_ID&from=2024-01-01&to=2024-01-31
   ```
   Or use the multi-sheet report export:
   ```
   https://YOUR_ANDON_IP:8090/api/export/reports.xlsx?screen=SCREEN_ID&from=2024-01-01&to=2024-01-31&report_type=all
   ```
   The `report_type=all` export creates one worksheet per report type (downtime, OEE, andon response, etc.).
2. In Power BI: **Get Data > Excel Workbook** and select the downloaded file.
3. Select the sheets to import and click **Load**.

## Finding Your Screen ID

Query the screens list:

```
GET https://YOUR_ANDON_IP:8090/api/screens
```

Response:

```json
[
  { "id": "a1b2c3d4-...", "name": "Assembly Line 1", "slug": "assembly-line-1" }
]
```

Use the `id` value as the `screen` parameter.

## Example Visuals

### 1. OEE Gauge per Operation

- **Visual**: Gauge or KPI card
- **Data source**: ShiftSummary query
- **Value**: Average of `oee`
- **Filter**: Single `shape_name`, single `shift_name`, today's `count_date`
- **Target**: 0.85 (85%) -- set as the gauge target line

### 2. Actual vs. Planned Bar Chart

- **Visual**: Clustered bar chart
- **Axis**: `shape_name`
- **Values**: Sum of `actual`, Sum of `planned`
- **Filter**: Single date, single shift
- Shows at a glance which operations are ahead or behind plan.

### 3. Downtime Pareto

- **Visual**: Stacked bar chart or waterfall
- **Data source**: ShiftSummary query
- **Axis**: `shape_name`
- **Values**: `downtime_seconds`, `starved_blocked_seconds`, `tool_change_seconds`, `operator_overcycle_seconds`, `machine_overcycle_seconds`
- **Sort**: Descending by total duration
- Identifies the largest loss contributors per operation.

### 4. OEE Trend Over Time

- **Visual**: Line chart
- **Axis**: `count_date`
- **Values**: Average of `oee`
- **Legend**: `shape_name`
- **Date range**: Last 30 days
- Shows OEE trends per operation over the selected period.

### 5. Andon Response Breakdown

Create a calculated table or use unpivot to transform the 10 andon columns into a category/value format:

In Power Query, select the 10 `andon_*_seconds` columns, then **Transform > Unpivot Columns**. This creates two new columns:
- `Attribute` (e.g. "andon_maint_seconds")
- `Value` (duration in seconds)

Use a **Pie chart** or **Treemap** with `Attribute` as the category and `Value` as the size.

### 6. Hourly Production Heatmap

- **Data source**: Use the hourly counts CSV endpoint
- **Visual**: Matrix or heatmap
- **Rows**: `shape_name`
- **Columns**: `shift_hour`
- **Values**: `actual`
- **Conditional formatting**: Color scale from red (0) to green (planned)

## DAX Measures

Useful calculated measures for Andon data:

```dax
// OEE as percentage
OEE % = AVERAGE(ShiftSummary[oee]) * 100

// Total downtime in minutes
Total Downtime (min) = SUM(ShiftSummary[downtime_seconds]) / 60

// Plan attainment percentage
Plan Attainment % = DIVIDE(SUM(ShiftSummary[actual]), SUM(ShiftSummary[planned]), 0) * 100

// Total andon time (all 10 categories)
Total Andon (min) = (
    SUM(ShiftSummary[andon_prod_seconds]) +
    SUM(ShiftSummary[andon_maint_seconds]) +
    SUM(ShiftSummary[andon_logistics_seconds]) +
    SUM(ShiftSummary[andon_quality_seconds]) +
    SUM(ShiftSummary[andon_hr_seconds]) +
    SUM(ShiftSummary[andon_emergency_seconds]) +
    SUM(ShiftSummary[andon_tooling_seconds]) +
    SUM(ShiftSummary[andon_engineering_seconds]) +
    SUM(ShiftSummary[andon_controls_seconds]) +
    SUM(ShiftSummary[andon_it_seconds])
) / 60

// Availability %
Availability % = AVERAGE(ShiftSummary[availability]) * 100
```

## Scheduled Refresh

To keep Power BI reports up to date automatically:

1. **Publish** the report to Power BI Service (app.powerbi.com)
2. Install an **On-premises Data Gateway** on a machine that can reach the Andon server
3. In Power BI Service, go to **Dataset settings > Gateway connection** and assign the gateway
4. Under **Scheduled refresh**, set the frequency (e.g. every 30 minutes during production hours)
5. Set credentials to **Anonymous** for the Andon web data source

> **Note**: The gateway machine must be able to resolve and connect to the Andon server's IP and port. If using self-signed certs, install the CA certificate on the gateway machine.

## Tips

- **Start with shift summary**: It contains all the key metrics pre-computed. You rarely need the hourly counts or reports endpoints for Power BI dashboards.
- **Use CSV for simplicity**: The CSV export endpoints return flat, well-typed data that Power BI handles without JSON parsing. Use `/api/export/shift-summary.csv` if the JSON approach feels complex.
- **Multi-sheet XLSX**: The `/api/export/reports.xlsx?report_type=all` endpoint produces a multi-sheet workbook with one tab per report type -- useful for comprehensive offline analysis.
- **Multiple screens**: Create one query per screen, or parameterize the screen ID for a multi-station dashboard.
- **Date slicers**: Add a date slicer visual and connect it to the `count_date` column for interactive date filtering.
