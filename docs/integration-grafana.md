# Grafana Integration

Connect Grafana to Andon's JSON API to build custom dashboards for OEE, downtime, production counts, and andon response tracking.

## Quick Start

1. Install the **Infinity** datasource plugin in Grafana (`yesoreyeram-infinity-datasource`)
2. Add a new Infinity datasource, set the base URL to your Andon server (e.g. `https://10.0.1.50:8090`)
3. If using a self-signed certificate, enable **Skip TLS Verify** in the datasource settings
4. Create a dashboard, add a panel, and query the Shift Summary API:
   - Type: **JSON**
   - Method: **GET**
   - URL: `/api/shift-summary?screen=YOUR_SCREEN_ID&from=${__from:date:YYYY-MM-DD}&to=${__to:date:YYYY-MM-DD}`
   - Root selector: (leave blank -- response is a JSON array)
5. Map columns to your visualization (e.g. `count_date`, `shape_name`, `oee`, `actual`, `planned`)

You now have a Grafana panel pulling live production data from Andon.

---

## Prerequisites

| Component | Version | Notes |
|-----------|---------|-------|
| Grafana | 9.0+ | OSS or Enterprise |
| Infinity Plugin | 2.0+ | Install via `grafana-cli plugins install yesoreyeram-infinity-datasource` |
| Andon Server | v4 | HTTPS on port 8090 (default) |

> **Network**: Grafana must be able to reach the Andon server over HTTPS. If running on a plant floor VLAN, ensure firewall rules allow Grafana's IP to connect to port 8090.

## Datasource Setup

### Install the Infinity Plugin

```bash
grafana-cli plugins install yesoreyeram-infinity-datasource
systemctl restart grafana-server
```

Or in Docker:

```bash
docker run -e "GF_INSTALL_PLUGINS=yesoreyeram-infinity-datasource" grafana/grafana
```

### Configure the Datasource

1. Go to **Configuration > Data Sources > Add data source**
2. Search for **Infinity** and select it
3. Fill in:
   - **Name**: `Andon` (or your preferred name)
   - **Base URL**: `https://YOUR_ANDON_IP:8090`
4. Under **Authentication**:
   - Leave as **No Authentication** (Andon read endpoints are public)
5. Under **TLS/SSL**:
   - Check **Skip TLS Verify** if the Andon server uses a self-signed certificate
6. Click **Save & Test**

## Finding Your Screen ID

Every query requires a `screen` parameter. To find your screen IDs:

```
GET https://YOUR_ANDON_IP:8090/api/screens
```

Response:

```json
[
  {
    "id": "a1b2c3d4-...",
    "name": "Assembly Line 1",
    "slug": "assembly-line-1"
  }
]
```

Use the `id` field as the `screen` parameter in all queries. You can also find it in the Andon dashboard URL: `/designer?screen=SCREEN_ID`.

## Andon API Endpoints for Grafana

| Endpoint | Best For | Refresh Rate |
|----------|----------|--------------|
| `/api/shift-summary` | Historical OEE, daily production totals, trend analysis | 5-15 min |
| `/api/hourly-counts` | Intra-shift hourly production vs. plan | 1-5 min |
| `/api/reports` | Downtime, overcycle, starved/blocked, andon response | 5-15 min |

### Shift Summary (Recommended Starting Point)

The shift summary endpoint returns pre-computed metrics per shape, per shift, per day. This is the most efficient endpoint for historical dashboards.

```
GET /api/shift-summary?screen=SCREEN_ID&from=2024-01-01&to=2024-01-31
```

Returns a flat JSON array -- each row is one shape/shift/style combination for one day:

```json
[
  {
    "screen_id": "a1b2c3d4-...",
    "shape_id": "e5f6a7b8-...",
    "shape_name": "OP-100",
    "shape_type": "process",
    "shift_name": "Day",
    "job_style": "1",
    "style_name": "Style A",
    "count_date": "2024-01-15",
    "actual": 360,
    "planned": 384,
    "early": 0,
    "overtime": 12,
    "availability": 0.95,
    "performance": 0.9375,
    "quality": 1.0,
    "oee": 0.891,
    "downtime_seconds": 960.0,
    "downtime_count": 8,
    "operator_entry_seconds": 120.0,
    "operator_overcycle_seconds": 0.0,
    "machine_overcycle_seconds": 45.5,
    "starved_blocked_seconds": 300.0,
    "tool_change_seconds": 600.0,
    "tool_change_count": 2,
    "red_rabbit_seconds": 0.0,
    "scrap_count": 0,
    "andon_prod_seconds": 0.0,
    "andon_maint_seconds": 180.0,
    "andon_logistics_seconds": 0.0,
    "andon_quality_seconds": 0.0,
    "andon_hr_seconds": 0.0,
    "andon_emergency_seconds": 0.0,
    "andon_tooling_seconds": 0.0,
    "andon_engineering_seconds": 0.0,
    "andon_controls_seconds": 0.0,
    "andon_it_seconds": 0.0,
    "total_work_seconds": 28800.0,
    "style_minutes": 480,
    "computed_at": "2024-01-15T20:30:00Z"
  }
]
```

Optional filters: `shift` (shift name), `shape` (shape ID).

### Hourly Counts

```
GET /api/hourly-counts?screen=SCREEN_ID&date=2024-01-15
```

Returns a nested structure. Use Infinity's **JSONPath** or **UQL** parser to extract the data you need. The key fields for charting are inside `operations[].shifts[].hours[]`:

```json
{
  "operations": [
    {
      "shape_id": "...",
      "name": "OP-100",
      "shifts": [
        {
          "name": "Day",
          "time_range": "06:00 - 14:00",
          "hours": [
            { "shift_hour": 1, "label": "6:00", "actual": 45, "planned": 48 },
            { "shift_hour": 2, "label": "7:00", "actual": 47, "planned": 48 }
          ],
          "total_actual": 360,
          "total_planned": 384
        }
      ],
      "grand_actual": 360,
      "grand_planned": 384
    }
  ],
  "hours": [
    { "shift_hour": 1, "index": 1, "label": "6:00", "start_min": 360 }
  ]
}
```

### Reports

```
GET /api/reports?screen=SCREEN_ID&report_type=oee&date=2024-01-15
```

Available report types: `downtime`, `operator_entry`, `operator_overcycle`, `machine_overcycle`, `starved_blocked`, `red_rabbit`, `tool_change`, `oee`, `andon_response`, `production_trend`, `tool_change_trend`, `fault_trend`.

See [reports.md](reports.md) for response structures.

## Example Dashboards

### 1. OEE Trend (Time Series)

Shows OEE percentage per operation over time.

**Panel type**: Time series

**Infinity query**:
- Type: JSON
- Method: GET
- URL: `/api/shift-summary?screen=SCREEN_ID&from=${__from:date:YYYY-MM-DD}&to=${__to:date:YYYY-MM-DD}`
- Columns:
  | Selector | Type | Alias |
  |----------|------|-------|
  | `count_date` | Timestamp | Time |
  | `shape_name` | String | Operation |
  | `oee` | Number | OEE |

**Transform**: Group by `Operation`, then apply "Convert field type" to make `OEE` a percentage (multiply by 100 in a calculated field or use value mapping).

**Grafana variable** (optional): Create a variable `$shape` from `/api/screens/SCREEN_ID/layout` to let users filter by operation. Add `&shape=$shape` to the query URL.

### 2. Actual vs. Planned (Bar Chart)

Compare actual parts to planned for today's shift.

**Panel type**: Bar chart

**Infinity query**:
- URL: `/api/shift-summary?screen=SCREEN_ID&from=${__from:date:YYYY-MM-DD}&to=${__to:date:YYYY-MM-DD}`
- Columns:
  | Selector | Type | Alias |
  |----------|------|-------|
  | `shape_name` | String | Operation |
  | `shift_name` | String | Shift |
  | `actual` | Number | Actual |
  | `planned` | Number | Planned |

### 3. Downtime Breakdown (Pie Chart)

Show downtime categories for a single day.

**Panel type**: Pie chart

**Infinity query**:
- URL: `/api/reports?screen=SCREEN_ID&report_type=andon_response&date=${__from:date:YYYY-MM-DD}`
- Root selector: `operations[0].shifts[0].andons`
- Columns:
  | Selector | Type | Alias |
  |----------|------|-------|
  | `label` | String | Category |
  | `seconds` | Number | Duration (s) |

### 4. Production Trend (Multi-Series Line)

Daily part counts over 30 days.

**Panel type**: Time series

**Infinity query**:
- URL: `/api/reports?screen=SCREEN_ID&report_type=production_trend&days=30`
- Parser: **Backend** (for nested datasets)
- This endpoint returns `labels[]` and `datasets[]` arrays designed for charting

### 5. Loss Waterfall (Table + Calculated Fields)

A table showing the loss breakdown for each operation.

**Panel type**: Table

**Infinity query**:
- URL: `/api/shift-summary?screen=SCREEN_ID&from=${__from:date:YYYY-MM-DD}&to=${__to:date:YYYY-MM-DD}`
- Columns:
  | Selector | Type | Alias |
  |----------|------|-------|
  | `shape_name` | String | Operation |
  | `shift_name` | String | Shift |
  | `downtime_seconds` | Number | Downtime (s) |
  | `operator_overcycle_seconds` | Number | Op. Overcycle (s) |
  | `machine_overcycle_seconds` | Number | Mch. Overcycle (s) |
  | `starved_blocked_seconds` | Number | Starved/Blocked (s) |
  | `tool_change_seconds` | Number | Tool Change (s) |
  | `andon_maint_seconds` | Number | Maint. Andon (s) |

## Using Grafana Variables with Andon

Variables make dashboards interactive. Here are useful variable definitions:

### Screen Selector

- **Type**: Query
- **Data source**: Infinity
- **Query**: JSON, GET `/api/screens`
- **Value field**: `id`
- **Display field**: `name`
- Use as `$screen` in all panel queries

### Shape Filter

- **Type**: Custom
- **Values**: Paste shape IDs and names from your layout (or use a JSON query against `/api/screens/$screen/layout` and parse shape names)
- Use as `&shape=$shape` appended to queries

### Shift Filter

- **Type**: Custom
- **Values**: `Day,Afternoon,Night` (match your configured shift names)
- Use as `&shift=$shift` appended to queries

## Dashboard Refresh

| Data Type | Recommended Interval | Notes |
|-----------|---------------------|-------|
| Shift summary | 5-15 minutes | Data is pre-computed every 30 min by background sweep |
| Hourly counts | 1-5 minutes | Updates as parts are counted |
| Reports | 5-15 minutes | Computed on request from event log |
| Trend reports | 15-60 minutes | Aggregates over many days |

Set the dashboard auto-refresh in the top-right time picker dropdown.

## Tips

- **Date range**: Use Grafana's built-in time range picker. The Infinity plugin supports `${__from:date:YYYY-MM-DD}` and `${__to:date:YYYY-MM-DD}` template variables to pass the selected range to Andon's `from`/`to` or `date` parameters.
- **Self-signed certs**: If you see TLS errors, enable "Skip TLS Verify" on the datasource. Alternatively, add the Andon server's CA certificate to Grafana's trust store.
- **Performance**: The shift summary endpoint is the most efficient -- it returns pre-computed data. Use it for historical dashboards. Use hourly counts and reports for detailed intra-shift analysis.
- **Multiple screens**: If you have multiple Andon stations, create one Grafana datasource per station or use a variable to switch between screen IDs.
- **Alerting**: Grafana supports alerts on panel queries. For example, alert when OEE drops below 0.70 or downtime exceeds a threshold.
